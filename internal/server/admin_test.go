package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// --- fakes ---

type adminMenuStep struct {
	choice     int
	back, exit bool
}

// fakeAdminPresenter pops scripted results and captures every view it is asked
// to render, so tests can assert on error lines and row content.
type fakeAdminPresenter struct {
	menu  []adminMenuStep
	lists []AdminListAction
	forms []AdminFormAction

	gotMenuErrs []string
	gotLists    []screens.AdminListView
	gotForms    []screens.AdminFormView
}

func (f *fakeAdminPresenter) AdminMenu(_ net.Conn, errMsg string) (int, bool, bool, error) {
	f.gotMenuErrs = append(f.gotMenuErrs, errMsg)
	if len(f.menu) == 0 {
		panic("unexpected AdminMenu call")
	}
	s := f.menu[0]
	f.menu = f.menu[1:]
	return s.choice, s.back, s.exit, nil
}

func (f *fakeAdminPresenter) AdminList(_ net.Conn, v screens.AdminListView) (AdminListAction, error) {
	f.gotLists = append(f.gotLists, v)
	if len(f.lists) == 0 {
		panic("unexpected AdminList call")
	}
	a := f.lists[0]
	f.lists = f.lists[1:]
	return a, nil
}

func (f *fakeAdminPresenter) AdminForm(_ net.Conn, v screens.AdminFormView) (AdminFormAction, error) {
	f.gotForms = append(f.gotForms, v)
	if len(f.forms) == 0 {
		panic("unexpected AdminForm call")
	}
	a := f.forms[0]
	f.forms = f.forms[1:]
	return a, nil
}

// newAdminFixture opens a real temp store seeded with: admin user "root"
// (member of ZZADMIN), user "alice" (member of "ops"), service "PROD"
// (host h, port 23) linked to ops. Returns the flow (wired to the fake
// presenter) and the ids.
func newAdminFixture(t *testing.T, p *fakeAdminPresenter) (*adminFlow, map[string]int64) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	ids := map[string]int64{}
	ids["root"], _ = st.CreateUser(ctx, "root", "h")
	ids["alice"], _ = st.CreateUser(ctx, "alice", "h")
	ids["zzadmin"], _ = st.CreateGroup(ctx, store.AdminGroup) // returns the migrated group's id
	ids["ops"], _ = st.CreateGroup(ctx, "ops")
	st.AddUserToGroup(ctx, ids["root"], ids["zzadmin"])
	st.AddUserToGroup(ctx, ids["alice"], ids["ops"])
	ids["prod"], _ = st.CreateService(ctx, "PROD", "h", 23, false, true)
	st.LinkGroupService(ctx, ids["ops"], ids["prod"])

	f := &adminFlow{
		store:     st,
		presenter: p,
		identity:  auth.Identity{UserID: ids["root"], Username: "root", Groups: []string{store.AdminGroup}},
	}
	return f, ids
}

// lastList returns the most recently captured list view.
func lastList(t *testing.T, p *fakeAdminPresenter) screens.AdminListView {
	t.Helper()
	if len(p.gotLists) == 0 {
		t.Fatal("no list views captured")
	}
	return p.gotLists[len(p.gotLists)-1]
}

// --- tests ---

func TestAdminFlowMenuBackAndExit(t *testing.T) {
	p := &fakeAdminPresenter{menu: []adminMenuStep{{back: true}}}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	p = &fakeAdminPresenter{menu: []adminMenuStep{{exit: true}}}
	f, _ = newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestPageBounds(t *testing.T) {
	cases := []struct {
		page, total            int
		wantPage, wantS, wantE int
		wantInfo               string
	}{
		{0, 0, 0, 0, 0, "ROW 0 OF 0"},
		{0, 3, 0, 0, 3, "ROW 1 TO 3 OF 3"},
		{-1, 3, 0, 0, 3, "ROW 1 TO 3 OF 3"}, // PF7 on page 0 underflows; clamp
		{0, 20, 0, 0, 14, "ROW 1 TO 14 OF 20"},
		{1, 20, 1, 14, 20, "ROW 15 TO 20 OF 20"},
		{5, 20, 1, 14, 20, "ROW 15 TO 20 OF 20"}, // clamped after deletions
	}
	for _, c := range cases {
		page, s, e, info := pageBounds(c.page, c.total)
		if page != c.wantPage || s != c.wantS || e != c.wantE || info != c.wantInfo {
			t.Errorf("pageBounds(%d,%d) = %d,%d,%d,%q want %d,%d,%d,%q",
				c.page, c.total, page, s, e, info, c.wantPage, c.wantS, c.wantE, c.wantInfo)
		}
	}
}

func TestAdminUserAddHappyPath(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []AdminListAction{{PF: 4}, {PF: 3}},
		forms: []AdminFormAction{{Values: map[string]string{
			screens.FieldUsername: "carol",
			screens.FieldPassword: "pw",
			screens.FieldRetype:   "pw",
		}}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	u, err := f.store.GetUserByUsername(ctx, "carol")
	if err != nil {
		t.Fatalf("carol not created: %v", err)
	}
	if u.PasswordHash == "pw" || u.PasswordHash == "" {
		t.Errorf("password stored unhashed: %q", u.PasswordHash)
	}
	// AdminStore is a superset of auth.UserStore, so the real auth path works.
	if _, err := auth.Authenticate(ctx, f.store, "carol", "pw"); err != nil {
		t.Errorf("authenticate with new password: %v", err)
	}
}

func TestAdminUserAddDuplicatePreservesInput(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []AdminListAction{{PF: 4}, {PF: 3}},
		forms: []AdminFormAction{
			{Values: map[string]string{screens.FieldUsername: "alice", screens.FieldPassword: "pw", screens.FieldRetype: "pw"}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	last := p.gotForms[len(p.gotForms)-1]
	if last.ErrMsg != "'alice' ALREADY EXISTS" {
		t.Errorf("errMsg = %q", last.ErrMsg)
	}
	if last.Fields[0].Value != "alice" {
		t.Errorf("username not preserved on re-render: %+v", last.Fields[0])
	}
}

func TestAdminUserAddPasswordMismatch(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []AdminListAction{{PF: 4}, {PF: 3}},
		forms: []AdminFormAction{
			{Values: map[string]string{screens.FieldUsername: "carol", screens.FieldPassword: "a", screens.FieldRetype: "b"}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	if msg := p.gotForms[len(p.gotForms)-1].ErrMsg; msg != "PASSWORDS DO NOT MATCH" {
		t.Errorf("errMsg = %q", msg)
	}
	if _, err := f.store.GetUserByUsername(context.Background(), "carol"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("carol should not exist: %v", err)
	}
}

func TestAdminSetPassword(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []AdminListAction{{Cmd: 'S', Row: 0}, {PF: 3}}, // S on alice
		forms: []AdminFormAction{{Values: map[string]string{
			screens.FieldPassword: "newpw", screens.FieldRetype: "newpw",
		}}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Authenticate(ctx, f.store, "alice", "newpw"); err != nil {
		t.Errorf("authenticate with new password: %v", err)
	}
}

func TestAdminDeleteUserConfirmFlow(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []AdminListAction{{Cmd: 'D', Row: 0}, {}, {PF: 3}}, // D alice, Enter confirms
	}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if msg := p.gotLists[1].ErrMsg; !strings.Contains(msg, "CONFIRM DELETE OF 'alice'") {
		t.Errorf("confirm prompt = %q", msg)
	}
	if _, err := f.store.GetUserByUsername(context.Background(), "alice"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("alice should be deleted: %v", err)
	}
}

func TestAdminDeleteUserCancel(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []AdminListAction{{Cmd: 'D', Row: 0}, {PF: 3}, {PF: 3}}, // PF3 cancels, stays
	}
	f, _ := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	if _, err := f.store.GetUserByUsername(context.Background(), "alice"); err != nil {
		t.Errorf("alice should survive cancel: %v", err)
	}
}

func TestAdminDeleteOwnAccountBlocked(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []AdminListAction{{Cmd: 'D', Row: 1}, {}, {PF: 3}}, // D on root (self)
	}
	f, _ := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	if msg := p.gotLists[2].ErrMsg; msg != "CANNOT DELETE YOUR OWN ACCOUNT" {
		t.Errorf("errMsg = %q", msg)
	}
	if _, err := f.store.GetUserByUsername(context.Background(), "root"); err != nil {
		t.Errorf("root should survive: %v", err)
	}
}

func TestAdminDeleteLastAdminBlocked(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []AdminListAction{{Cmd: 'D', Row: 1}, {}, {PF: 3}}, // alice deletes root
	}
	f, ids := newAdminFixture(t, p)
	f.identity = auth.Identity{UserID: ids["alice"], Username: "alice", Groups: []string{store.AdminGroup}}
	f.Run(context.Background(), nil)
	if msg := p.gotLists[2].ErrMsg; !strings.Contains(msg, "CANNOT REMOVE LAST") {
		t.Errorf("errMsg = %q", msg)
	}
	if _, err := f.store.GetUserByUsername(context.Background(), "root"); err != nil {
		t.Errorf("root should survive: %v", err)
	}
}

func TestAdminUserListPaging(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []AdminListAction{{PF: 8}, {PF: 7}, {PF: 3}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	for i := 0; i < 20; i++ { // 22 users total incl. root + alice
		f.store.CreateUser(ctx, fmt.Sprintf("user%02d", i), "h")
	}
	f.Run(ctx, nil)
	wants := []string{"ROW 1 TO 14 OF 22", "ROW 15 TO 22 OF 22", "ROW 1 TO 14 OF 22"}
	for i, want := range wants {
		if got := p.gotLists[i].RowInfo; got != want {
			t.Errorf("render %d RowInfo = %q, want %q", i, got, want)
		}
	}
}

func TestAdminDeleteUserOtherActionCancelsConfirm(t *testing.T) {
	// D on alice, then PF8 (page): confirm silently cancelled, no delete.
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []AdminListAction{{Cmd: 'D', Row: 0}, {PF: 8}, {PF: 3}},
	}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetUserByUsername(context.Background(), "alice"); err != nil {
		t.Errorf("alice should survive a non-Enter action after D: %v", err)
	}
}

func TestAdminUserGroupsToggle(t *testing.T) {
	// Group rows sort ZZADMIN(0), ops(1). Add alice to ZZADMIN, remove from ops.
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 1}, {back: true}},
		lists: []AdminListAction{
			{Cmd: 'G', Row: 0}, // users list: G on alice
			{Cmd: 'A', Row: 0}, // add ZZADMIN
			{Cmd: 'R', Row: 1}, // remove ops
			{PF: 3},            // back to users list
			{PF: 3},            // back to admin menu
		},
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := f.store.GetUserGroups(ctx, ids["alice"])
	if len(got) != 1 || got[0] != store.AdminGroup {
		t.Errorf("alice groups = %v, want [%s]", got, store.AdminGroup)
	}
	// membership marker rendered: second list render shows ops marked X for alice
	if rows := p.gotLists[1].Rows; len(rows) != 2 || !strings.Contains(rows[1], "X") {
		t.Errorf("ops row should carry X marker: %q", rows)
	}
}

func TestAdminRemoveLastAdminMembershipBlocked(t *testing.T) {
	// root is ZZADMIN's only member; R must be blocked.
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 1}, {back: true}},
		lists: []AdminListAction{
			{Cmd: 'G', Row: 1}, // users list: G on root
			{Cmd: 'R', Row: 0}, // remove ZZADMIN — blocked
			{PF: 3}, {PF: 3},
		},
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	f.Run(ctx, nil)
	if msg := p.gotLists[2].ErrMsg; !strings.Contains(msg, "CANNOT REMOVE LAST") {
		t.Errorf("errMsg = %q", msg)
	}
	got, _ := f.store.GetUserGroups(ctx, ids["root"])
	if len(got) != 1 || got[0] != store.AdminGroup {
		t.Errorf("root memberships changed: %v", got)
	}
}

func TestAdminGroupAddAndCounts(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 2}, {back: true}},
		lists: []AdminListAction{{PF: 4}, {PF: 3}},
		forms: []AdminFormAction{{Values: map[string]string{screens.FieldName: "dev"}}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	groups, _ := f.store.ListGroups(ctx)
	// sort: ZZADMIN, dev, ops
	if len(groups) != 3 || groups[1].Name != "dev" {
		t.Fatalf("groups = %+v", groups)
	}
	// the re-rendered list shows member/service counts; ops row has 1 and 1
	last := lastList(t, p)
	if len(last.Rows) != 3 || !strings.Contains(last.Rows[2], "1") {
		t.Errorf("ops row missing counts: %q", last.Rows)
	}
}

func TestAdminGroupAddReservedBlocked(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 2}, {back: true}},
		lists: []AdminListAction{{PF: 4}, {PF: 3}},
		forms: []AdminFormAction{
			{Values: map[string]string{screens.FieldName: "zzNew"}}, // prefix check is case-insensitive
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	if msg := p.gotForms[1].ErrMsg; msg != "ZZ* GROUP NAMES ARE RESERVED" {
		t.Errorf("errMsg = %q", msg)
	}
}

func TestAdminGroupAddDuplicateBlocked(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 2}, {back: true}},
		lists: []AdminListAction{{PF: 4}, {PF: 3}},
		forms: []AdminFormAction{
			{Values: map[string]string{screens.FieldName: "ops"}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	if msg := p.gotForms[1].ErrMsg; msg != "'ops' ALREADY EXISTS" {
		t.Errorf("errMsg = %q", msg)
	}
}

func TestAdminGroupDeleteReservedBlocked(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 2}, {back: true}},
		lists: []AdminListAction{{Cmd: 'D', Row: 0}, {PF: 3}}, // D on ZZADMIN — no confirm offered
	}
	f, _ := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	if msg := p.gotLists[1].ErrMsg; msg != "ZZ* GROUP NAMES ARE RESERVED" {
		t.Errorf("errMsg = %q", msg)
	}
	if groups, _ := f.store.ListGroups(context.Background()); len(groups) != 2 {
		t.Errorf("groups = %+v", groups)
	}
}

func TestAdminGroupDeleteCascades(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 2}, {back: true}},
		lists: []AdminListAction{{Cmd: 'D', Row: 1}, {}, {PF: 3}}, // D ops, Enter confirms
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.store.GetUserGroups(ctx, ids["alice"]); len(got) != 0 {
		t.Errorf("alice memberships = %v", got)
	}
	if gs, _ := f.store.ListGroupsForService(ctx, ids["prod"]); len(gs) != 0 {
		t.Errorf("PROD links = %v", gs)
	}
}

func TestAdminGroupDeleteOtherActionCancelsConfirm(t *testing.T) {
	// D on ops, then PF8 (page): confirm silently cancelled, no delete.
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 2}, {back: true}},
		lists: []AdminListAction{{Cmd: 'D', Row: 1}, {PF: 8}, {PF: 3}},
	}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if groups, _ := f.store.ListGroups(context.Background()); len(groups) != 2 {
		t.Errorf("ops should survive a non-Enter action after D: %+v", groups)
	}
}

func TestAdminGroupDeleteCancel(t *testing.T) {
	// D on ops, then PF3: confirm cancelled, stays on list, no delete.
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 2}, {back: true}},
		lists: []AdminListAction{{Cmd: 'D', Row: 1}, {PF: 3}, {PF: 3}},
	}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if groups, _ := f.store.ListGroups(context.Background()); len(groups) != 2 {
		t.Errorf("ops should survive PF3 cancel: %+v", groups)
	}
}

func TestAdminServiceAdd(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 3}, {back: true}},
		lists: []AdminListAction{{PF: 4}, {PF: 3}},
		forms: []AdminFormAction{{Values: map[string]string{
			screens.FieldName: "DEV", screens.FieldHost: "dev.example", screens.FieldPort: "992",
			screens.FieldTLS: "y", screens.FieldVerify: "n", // case-insensitive Y/N
		}}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	svcs, _ := f.store.ListAllServices(ctx)
	if len(svcs) != 2 || svcs[0].Name != "DEV" {
		t.Fatalf("services = %+v", svcs)
	}
	got := svcs[0]
	if got.Host != "dev.example" || got.Port != 992 || !got.TLS || got.TLSVerify {
		t.Errorf("DEV = %+v", got)
	}
}

func TestAdminServiceEditPrefillAndUpdate(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 3}, {back: true}},
		lists: []AdminListAction{{Cmd: 'S', Row: 0}, {PF: 3}}, // edit PROD
		forms: []AdminFormAction{{Values: map[string]string{
			screens.FieldName: "PROD", screens.FieldHost: "h2", screens.FieldPort: "1023",
			screens.FieldTLS: "Y", screens.FieldVerify: "Y",
		}}},
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	form := p.gotForms[0] // pre-filled from the existing service
	if form.Fields[0].Value != "PROD" || form.Fields[1].Value != "h" || form.Fields[2].Value != "23" {
		t.Errorf("pre-fill = %+v", form.Fields)
	}
	svc, _ := f.store.GetService(ctx, ids["prod"])
	if svc.Host != "h2" || svc.Port != 1023 || !svc.TLS || !svc.TLSVerify {
		t.Errorf("updated = %+v", svc)
	}
}

func TestAdminServicePortValidation(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 3}, {back: true}},
		lists: []AdminListAction{{PF: 4}, {PF: 3}},
		forms: []AdminFormAction{
			{Values: map[string]string{screens.FieldName: "X", screens.FieldHost: "h",
				screens.FieldPort: "70000", screens.FieldTLS: "N", screens.FieldVerify: "Y"}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	if msg := p.gotForms[1].ErrMsg; msg != "PORT MUST BE 1-65535" {
		t.Errorf("errMsg = %q", msg)
	}
}

func TestAdminServiceDuplicateName(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 3}, {back: true}},
		lists: []AdminListAction{{PF: 4}, {PF: 3}},
		forms: []AdminFormAction{
			{Values: map[string]string{screens.FieldName: "PROD", screens.FieldHost: "h",
				screens.FieldPort: "23", screens.FieldTLS: "N", screens.FieldVerify: "Y"}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	if msg := p.gotForms[1].ErrMsg; msg != "'PROD' ALREADY EXISTS" {
		t.Errorf("errMsg = %q", msg)
	}
}

func TestAdminServiceDeleteCascades(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 3}, {back: true}},
		lists: []AdminListAction{{Cmd: 'D', Row: 0}, {}, {PF: 3}}, // D PROD, Enter confirms
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetService(ctx, ids["prod"]); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("PROD should be deleted: %v", err)
	}
}

func TestAdminServiceDeleteCancel(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 3}, {back: true}},
		lists: []AdminListAction{{Cmd: 'D', Row: 0}, {PF: 3}, {PF: 3}},
	}
	f, ids := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	if _, err := f.store.GetService(context.Background(), ids["prod"]); err != nil {
		t.Errorf("PROD should survive PF3 cancel: %v", err)
	}
}

func TestAdminServiceDeleteOtherActionCancelsConfirm(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 3}, {back: true}},
		lists: []AdminListAction{{Cmd: 'D', Row: 0}, {PF: 8}, {PF: 3}},
	}
	f, ids := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	if _, err := f.store.GetService(context.Background(), ids["prod"]); err != nil {
		t.Errorf("PROD should survive a non-Enter action after D: %v", err)
	}
}

func TestAdminServiceFormPreservesInputOnError(t *testing.T) {
	// Bad port: every other typed value must come back pre-filled.
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 3}, {back: true}},
		lists: []AdminListAction{{PF: 4}, {PF: 3}},
		forms: []AdminFormAction{
			{Values: map[string]string{screens.FieldName: "DEV", screens.FieldHost: "dev.example",
				screens.FieldPort: "junk", screens.FieldTLS: "y", screens.FieldVerify: "n"}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	last := p.gotForms[len(p.gotForms)-1]
	if last.ErrMsg != "PORT MUST BE 1-65535" {
		t.Errorf("errMsg = %q", last.ErrMsg)
	}
	wants := []string{"DEV", "dev.example", "junk", "Y", "N"} // Y/N canonicalized upper
	for i, want := range wants {
		if last.Fields[i].Value != want {
			t.Errorf("field %d preserved = %q, want %q", i, last.Fields[i].Value, want)
		}
	}
}

func TestAdminServiceGroupsToggle(t *testing.T) {
	// Group rows sort ZZADMIN(0), ops(1). Grant ZZADMIN access to PROD, revoke ops.
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 3}, {back: true}},
		lists: []AdminListAction{
			{Cmd: 'G', Row: 0}, // services list: G on PROD
			{Cmd: 'A', Row: 0}, // grant ZZADMIN
			{Cmd: 'R', Row: 1}, // revoke ops
			{PF: 3},            // back to services list
			{PF: 3},            // back to admin menu
		},
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	groups, _ := f.store.ListGroupsForService(ctx, ids["prod"])
	if len(groups) != 1 || groups[0].Name != store.AdminGroup {
		t.Errorf("PROD groups = %+v, want [%s]", groups, store.AdminGroup)
	}
	// access marker rendered: ops row carries X on the first toggle render
	if rows := p.gotLists[1].Rows; len(rows) != 2 || !strings.Contains(rows[1], "X") {
		t.Errorf("ops row should carry X marker: %q", rows)
	}
}
