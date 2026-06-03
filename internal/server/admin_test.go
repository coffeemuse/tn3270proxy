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

