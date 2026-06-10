/*
 * Copyright 2026 by CoffeeMuse.
 *
 * This file is part of tn3270proxy.
 *
 * tn3270proxy is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * tn3270proxy is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with tn3270proxy. If not, see <https://www.gnu.org/licenses/>.
 */

package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/coffeemuse/tn3270proxy/internal/auth"
	"github.com/coffeemuse/tn3270proxy/internal/screens"
	"github.com/coffeemuse/tn3270proxy/internal/store"
	"github.com/coffeemuse/tn3270proxy/internal/ui3270"
)

// --- fakes ---

type adminMenuStep struct {
	choice int
	back   bool
}

// fakeAdminPresenter pops scripted results and captures every view it is asked
// to render, so tests can assert on error lines and row content.
type fakeAdminPresenter struct {
	menu  []adminMenuStep
	lists []ui3270.ListAction
	forms []ui3270.FormAction

	gotMenuErrs []string
	gotLists    []ui3270.ListView
	gotForms    []ui3270.FormView

	snaps    []ui3270.ListAction
	gotSnaps []ui3270.SnapshotView
	gotDets  []ui3270.DetailView
	detActs  []ui3270.ListAction
}

func (f *fakeAdminPresenter) AdminMenu(_ net.Conn, _ Term, errMsg string) (int, bool, error) {
	f.gotMenuErrs = append(f.gotMenuErrs, errMsg)
	if len(f.menu) == 0 {
		panic("unexpected AdminMenu call")
	}
	s := f.menu[0]
	f.menu = f.menu[1:]
	return s.choice, s.back, nil
}

func (f *fakeAdminPresenter) List(v ui3270.ListView) (ui3270.ListAction, error) {
	f.gotLists = append(f.gotLists, v)
	if len(f.lists) == 0 {
		panic("unexpected List call")
	}
	a := f.lists[0]
	f.lists = f.lists[1:]
	return a, nil
}

func (f *fakeAdminPresenter) Form(v ui3270.FormView) (ui3270.FormAction, error) {
	// Snapshot Fields: glue may re-seed the same backing slice between renders
	// to preserve typed-but-rejected input, so capture a copy per render.
	v.Fields = append([]ui3270.FormField(nil), v.Fields...)
	f.gotForms = append(f.gotForms, v)
	if len(f.forms) == 0 {
		panic("unexpected Form call")
	}
	a := f.forms[0]
	f.forms = f.forms[1:]
	return a, nil
}

func (f *fakeAdminPresenter) Snapshot(v ui3270.SnapshotView) (ui3270.ListAction, error) {
	f.gotSnaps = append(f.gotSnaps, v)
	if len(f.snaps) == 0 {
		panic("unexpected Snapshot call")
	}
	a := f.snaps[0]
	f.snaps = f.snaps[1:]
	return a, nil
}

func (f *fakeAdminPresenter) Detail(v ui3270.DetailView) error {
	f.gotDets = append(f.gotDets, v)
	return nil
}

func (f *fakeAdminPresenter) DetailAct(v ui3270.DetailView, _ int) (ui3270.ListAction, error) {
	f.gotDets = append(f.gotDets, v)
	if len(f.detActs) == 0 {
		panic("unexpected DetailAct call")
	}
	a := f.detActs[0]
	f.detActs = f.detActs[1:]
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
	ids["prod"], _ = st.CreateService(ctx, "PROD", "Production", "h", 23, false, true)
	st.LinkGroupService(ctx, ids["ops"], ids["prod"])

	f := &adminFlow{
		store:     st,
		presenter: p, // AdminMenu only
		renderer:  func(net.Conn) ui3270.Renderer { return p },
		identity:  auth.Identity{UserID: ids["root"], Username: "root", Groups: []string{store.AdminGroup}},
		term:      Term{Type: "IBM-3278-2", Rows: 24, Cols: 80},
	}
	return f, ids
}

// lastList returns the most recently captured list view.
func lastList(t *testing.T, p *fakeAdminPresenter) ui3270.ListView {
	t.Helper()
	if len(p.gotLists) == 0 {
		t.Fatal("no list views captured")
	}
	return p.gotLists[len(p.gotLists)-1]
}

// --- tests ---

func TestAdminFlowMenuBack(t *testing.T) {
	p := &fakeAdminPresenter{menu: []adminMenuStep{{back: true}}}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestAdminRun_Choice6DispatchesAudit(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 6}, {back: true}},
		snaps: []ui3270.ListAction{{PF: 3}}, // audit list opens then PF3 back
	}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(p.gotSnaps) != 1 {
		t.Errorf("expected one Snapshot render from the audit flow, got %d", len(p.gotSnaps))
	}
}

func TestAdminUserAddHappyPath(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{{Values: map[string]string{
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
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{
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

func TestAdminUserAddInvalidEmailPreservesUsername(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}}, // PF4 = add (create mode)
		forms: []ui3270.FormAction{
			{Values: map[string]string{
				screens.FieldUsername: "carol",
				screens.FieldEmail:    "not-an-email",
				screens.FieldPassword: "pw",
				screens.FieldRetype:   "pw",
			}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	last := p.gotForms[len(p.gotForms)-1]
	if last.ErrMsg == "" {
		t.Errorf("expected validation error, got none")
	}
	if last.Fields[0].Value != "carol" {
		t.Errorf("username not preserved on validation error: %+v", last.Fields[0])
	}
}

func TestAdminUserAddPasswordMismatch(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{
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

func TestAdminUserAddPasswordTooLong(t *testing.T) {
	long := strings.Repeat("a", 73) // > bcrypt's 72-byte limit
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{
			{Values: map[string]string{screens.FieldUsername: "dave", screens.FieldPassword: long, screens.FieldRetype: long}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	if msg := p.gotForms[len(p.gotForms)-1].ErrMsg; msg != "PASSWORD TOO LONG (MAX 72 BYTES)" {
		t.Errorf("errMsg = %q", msg)
	}
	if _, err := f.store.GetUserByUsername(context.Background(), "dave"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("dave should not exist: %v", err)
	}
}

func TestAdminSetPassword(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}}, // S on alice
		forms: []ui3270.FormAction{{Values: map[string]string{
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

func TestAdminEditUserDetails(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}}, // S on alice (row 0)
		forms: []ui3270.FormAction{{Values: map[string]string{
			screens.FieldFullName: "Alice Doe",
			screens.FieldEmail:    "Alice@Example.COM",
			// password + retype blank → keep current
		}}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	u, err := f.store.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	if u.FullName != "Alice Doe" || u.Email != "alice@example.com" {
		t.Errorf("details = %q/%q", u.FullName, u.Email)
	}
	if u.PasswordHash != "h" {
		t.Errorf("blank password should keep current hash, got %q", u.PasswordHash)
	}
}

func TestAdminEditUserChangesPassword(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}},
		forms: []ui3270.FormAction{{Values: map[string]string{
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

func TestAdminEditUserUsernameReadOnly(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}},
		forms: []ui3270.FormAction{{Cancel: true}},
	}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	form := p.gotForms[len(p.gotForms)-1]
	var uf *ui3270.FormField
	for i := range form.Fields {
		if form.Fields[i].Name == screens.FieldUsername {
			uf = &form.Fields[i]
		}
	}
	if uf == nil {
		t.Fatal("username field missing from edit form")
	}
	if !uf.ReadOnly {
		t.Errorf("username should be read-only on edit")
	}
	if uf.Value != "ALICE" {
		t.Errorf("username value = %q, want ALICE", uf.Value)
	}
}

func TestAdminEditUserInvalidEmail(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}},
		forms: []ui3270.FormAction{
			{Values: map[string]string{screens.FieldEmail: "not-an-email"}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if msg := p.gotForms[len(p.gotForms)-1].ErrMsg; msg == "" {
		t.Errorf("expected a validation error message, got empty")
	}
}

func TestAdminDeleteUserConfirmFlow(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'D', Row: 0}, {}, {PF: 3}}, // D alice, Enter confirms
	}
	f, _ := newAdminFixture(t, p)
	if err := f.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if msg := p.gotLists[1].ErrMsg; !strings.Contains(msg, "CONFIRM DELETE OF 'ALICE'") {
		t.Errorf("confirm prompt = %q", msg)
	}
	if _, err := f.store.GetUserByUsername(context.Background(), "alice"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("alice should be deleted: %v", err)
	}
}

func TestAdminDeleteUserCancel(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'D', Row: 0}, {PF: 3}, {PF: 3}}, // PF3 cancels, stays
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
		lists: []ui3270.ListAction{{Cmd: 'D', Row: 1}, {}, {PF: 3}}, // D on root (self)
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
		lists: []ui3270.ListAction{{Cmd: 'D', Row: 1}, {}, {PF: 3}}, // alice deletes root
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
		lists: []ui3270.ListAction{{PF: 8}, {PF: 7}, {PF: 3}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	for i := 0; i < 20; i++ { // 22 users total incl. root + alice
		f.store.CreateUser(ctx, fmt.Sprintf("user%02d", i), "h")
	}
	f.Run(ctx, nil)
	ps := f.term.Geometry().ListPageSize() // 14 for the fixture's 24×80 term
	wants := []string{
		fmt.Sprintf("ROW 1 TO %d OF 22", ps),
		fmt.Sprintf("ROW %d TO 22 OF 22", ps+1),
		fmt.Sprintf("ROW 1 TO %d OF 22", ps),
	}
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
		lists: []ui3270.ListAction{{Cmd: 'D', Row: 0}, {PF: 8}, {PF: 3}},
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
	// Group rows sort OPS(0), ZZADMIN(1) (all uppercase, lexicographic). Add alice to ZZADMIN, remove from OPS.
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{
			{Cmd: 'G', Row: 0}, // users list: G on alice
			{Cmd: 'A', Row: 1}, // add ZZADMIN (row 1)
			{Cmd: 'R', Row: 0}, // remove OPS (row 0)
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
	// membership marker rendered: second list render shows OPS marked X for alice (row 0)
	if rows := p.gotLists[1].Rows; len(rows) != 2 || !strings.Contains(rows[0], "X") {
		t.Errorf("OPS row should carry X marker: %q", rows)
	}
}

func TestAdminRemoveLastAdminMembershipBlocked(t *testing.T) {
	// root is ZZADMIN's only member; R must be blocked.
	// Group rows sort OPS(0), ZZADMIN(1).
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{
			{Cmd: 'G', Row: 1}, // users list: G on root
			{Cmd: 'R', Row: 1}, // remove ZZADMIN (row 1) — blocked
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
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{{Values: map[string]string{screens.FieldName: "dev"}}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	groups, _ := f.store.ListGroups(ctx)
	// sort (all uppercase): DEV, OPS, ZZADMIN
	if len(groups) != 3 || groups[0].Name != "DEV" {
		t.Fatalf("groups = %+v", groups)
	}
	// the re-rendered list shows member/service counts; OPS row (index 1) has 1 and 1
	last := lastList(t, p)
	if len(last.Rows) != 3 || !strings.Contains(last.Rows[1], "1") {
		t.Errorf("OPS row missing counts: %q", last.Rows)
	}
}

func TestAdminGroupAddReservedBlocked(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 2}, {back: true}},
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{
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
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{
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

func TestAdminGroupAddReSeedsInputOnError(t *testing.T) {
	// Submit a reserved group name; the form should re-render with the typed
	// name pre-filled in the name field (gotForms[1].Fields[0].Value == "zzbad").
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 2}, {back: true}},
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{
			{Values: map[string]string{screens.FieldName: "zzbad"}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	if len(p.gotForms) < 2 {
		t.Fatalf("expected at least 2 form renders, got %d", len(p.gotForms))
	}
	second := p.gotForms[1]
	if second.ErrMsg != "ZZ* GROUP NAMES ARE RESERVED" {
		t.Errorf("errMsg = %q, want ZZ* GROUP NAMES ARE RESERVED", second.ErrMsg)
	}
	if second.Fields[0].Value != "zzbad" {
		t.Errorf("name field not re-seeded: got %q, want %q", second.Fields[0].Value, "zzbad")
	}
}

func TestAdminGroupDeleteReservedBlocked(t *testing.T) {
	// Group rows sort OPS(0), ZZADMIN(1).
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 2}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'D', Row: 1}, {PF: 3}}, // D on ZZADMIN (row 1) — no confirm offered
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
	// Group rows sort OPS(0), ZZADMIN(1).
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 2}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'D', Row: 0}, {}, {PF: 3}}, // D OPS (row 0), Enter confirms
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
		lists: []ui3270.ListAction{{Cmd: 'D', Row: 1}, {PF: 8}, {PF: 3}},
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
		lists: []ui3270.ListAction{{Cmd: 'D', Row: 1}, {PF: 3}, {PF: 3}},
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
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{{Values: map[string]string{
			screens.FieldName: "DEV", screens.FieldDescription: "Dev environment", screens.FieldHost: "dev.example", screens.FieldPort: "992",
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
		lists: []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}}, // edit PROD
		forms: []ui3270.FormAction{{Values: map[string]string{
			screens.FieldName: "PROD", screens.FieldDescription: "Production v2", screens.FieldHost: "h2", screens.FieldPort: "1023",
			screens.FieldTLS: "Y", screens.FieldVerify: "Y",
		}}},
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	form := p.gotForms[0] // pre-filled from the existing service
	// Fields: 0=Name, 1=Description, 2=Host, 3=Port
	if form.Fields[0].Value != "PROD" || form.Fields[1].Value != "Production" || form.Fields[2].Value != "h" || form.Fields[3].Value != "23" {
		t.Errorf("pre-fill = %+v", form.Fields)
	}
	svc, _ := f.store.(*store.Store).GetService(ctx, ids["prod"])
	if svc.Host != "h2" || svc.Port != 1023 || !svc.TLS || !svc.TLSVerify {
		t.Errorf("updated = %+v", svc)
	}
	if svc.Description != "Production v2" {
		t.Errorf("updated Description = %q, want %q", svc.Description, "Production v2")
	}
}

func TestAdminServicePortValidation(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 3}, {back: true}},
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{
			{Values: map[string]string{screens.FieldName: "X", screens.FieldDescription: "Svc X", screens.FieldHost: "h",
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
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{
			{Values: map[string]string{screens.FieldName: "PROD", screens.FieldDescription: "Production", screens.FieldHost: "h",
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

func TestAdminServiceNameAndDescriptionValidation(t *testing.T) {
	t.Run("invalid name with space", func(t *testing.T) {
		p := &fakeAdminPresenter{
			menu:  []adminMenuStep{{choice: 3}, {back: true}},
			lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
			forms: []ui3270.FormAction{
				{Values: map[string]string{
					screens.FieldName:        "BAD NAME",
					screens.FieldDescription: "Valid description",
					screens.FieldHost:        "h",
					screens.FieldPort:        "23",
					screens.FieldTLS:         "N",
					screens.FieldVerify:      "Y",
				}},
				{Cancel: true},
			},
		}
		f, _ := newAdminFixture(t, p)
		ctx := context.Background()
		f.Run(ctx, nil)
		if msg := p.gotForms[1].ErrMsg; !strings.Contains(msg, "SERVICE NAME") {
			t.Errorf("errMsg = %q, want message containing SERVICE NAME", msg)
		}
		svcs, _ := f.store.ListAllServices(ctx)
		for _, s := range svcs {
			if s.Name != "PROD" {
				t.Errorf("unexpected service persisted: %+v", s)
			}
		}
	})

	t.Run("empty description", func(t *testing.T) {
		p := &fakeAdminPresenter{
			menu:  []adminMenuStep{{choice: 3}, {back: true}},
			lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
			forms: []ui3270.FormAction{
				{Values: map[string]string{
					screens.FieldName:        "OK",
					screens.FieldDescription: "",
					screens.FieldHost:        "h",
					screens.FieldPort:        "23",
					screens.FieldTLS:         "N",
					screens.FieldVerify:      "Y",
				}},
				{Cancel: true},
			},
		}
		f, _ := newAdminFixture(t, p)
		ctx := context.Background()
		f.Run(ctx, nil)
		if msg := p.gotForms[1].ErrMsg; !strings.Contains(msg, "DESCRIPTION") {
			t.Errorf("errMsg = %q, want message containing DESCRIPTION", msg)
		}
		svcs, _ := f.store.ListAllServices(ctx)
		for _, s := range svcs {
			if s.Name != "PROD" {
				t.Errorf("unexpected service persisted: %+v", s)
			}
		}
	})
}

func TestAdminServiceDeleteCascades(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 3}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'D', Row: 0}, {}, {PF: 3}}, // D PROD, Enter confirms
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.(*store.Store).GetService(ctx, ids["prod"]); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("PROD should be deleted: %v", err)
	}
}

func TestAdminServiceDeleteCancel(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 3}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'D', Row: 0}, {PF: 3}, {PF: 3}},
	}
	f, ids := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	if _, err := f.store.(*store.Store).GetService(context.Background(), ids["prod"]); err != nil {
		t.Errorf("PROD should survive PF3 cancel: %v", err)
	}
}

func TestAdminServiceDeleteOtherActionCancelsConfirm(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 3}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'D', Row: 0}, {PF: 8}, {PF: 3}},
	}
	f, ids := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	if _, err := f.store.(*store.Store).GetService(context.Background(), ids["prod"]); err != nil {
		t.Errorf("PROD should survive a non-Enter action after D: %v", err)
	}
}

func TestAdminServiceFormPreservesInputOnError(t *testing.T) {
	// Bad port: every other typed value must come back pre-filled.
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 3}, {back: true}},
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{
			{Values: map[string]string{screens.FieldName: "DEV", screens.FieldDescription: "Dev environment", screens.FieldHost: "dev.example",
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
	// Fields: 0=Name, 1=Description, 2=Host, 3=Port, 4=TLS, 5=Verify; Y/N canonicalized upper
	wants := []string{"DEV", "Dev environment", "dev.example", "junk", "Y", "N"}
	for i, want := range wants {
		if last.Fields[i].Value != want {
			t.Errorf("field %d preserved = %q, want %q", i, last.Fields[i].Value, want)
		}
	}
}

func TestAdminServiceGroupsToggle(t *testing.T) {
	// Group rows sort OPS(0), ZZADMIN(1). Grant ZZADMIN access to PROD, revoke OPS.
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 3}, {back: true}},
		lists: []ui3270.ListAction{
			{Cmd: 'G', Row: 0}, // services list: G on PROD
			{Cmd: 'A', Row: 1}, // grant ZZADMIN (row 1)
			{Cmd: 'R', Row: 0}, // revoke OPS (row 0)
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
	// access marker rendered: OPS row (row 0) carries X on the first toggle render
	if rows := p.gotLists[1].Rows; len(rows) != 2 || !strings.Contains(rows[0], "X") {
		t.Errorf("OPS row should carry X marker: %q", rows)
	}
}

func TestAdminGroupMembersToggle(t *testing.T) {
	// Group rows sort OPS(0), ZZADMIN(1); user rows sort ALICE(0), ROOT(1).
	// M on OPS, add ROOT, remove ALICE.
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 2}, {back: true}},
		lists: []ui3270.ListAction{
			{Cmd: 'M', Row: 0}, // groups list: M on OPS (row 0)
			{Cmd: 'A', Row: 1}, // add ROOT (row 1)
			{Cmd: 'R', Row: 0}, // remove ALICE (row 0)
			{PF: 3},            // back to groups list
			{PF: 3},            // back to admin menu
		},
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	members, _ := f.store.ListUsersInGroup(ctx, ids["ops"])
	if len(members) != 1 || members[0].Username != "ROOT" {
		t.Errorf("ops members = %+v, want [ROOT]", members)
	}
	// groups list advertises the new command
	if legend := p.gotLists[0].Legend; !strings.Contains(legend, "M = members") {
		t.Errorf("groups legend = %q", legend)
	}
	// first members render: ALICE carries the X marker, ROOT does not
	if rows := p.gotLists[1].Rows; len(rows) != 2 ||
		!strings.Contains(rows[0], "X") || strings.Contains(rows[1], "X") {
		t.Errorf("member markers wrong: %q", rows)
	}
	if title := p.gotLists[1].Title; !strings.Contains(title, "MEMBERS OF OPS") {
		t.Errorf("title = %q", title)
	}
}

func TestAdminGroupMembersLastAdminGuard(t *testing.T) {
	// root is ZZADMIN's only member; R from the members side must be blocked.
	// Group rows sort OPS(0), ZZADMIN(1); user rows sort ALICE(0), ROOT(1).
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 2}, {back: true}},
		lists: []ui3270.ListAction{
			{Cmd: 'M', Row: 1}, // groups list: M on ZZADMIN (row 1)
			{Cmd: 'R', Row: 1}, // user rows ALICE(0), ROOT(1): remove ROOT — blocked
			{PF: 3}, {PF: 3},
		},
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	f.Run(ctx, nil)
	if msg := p.gotLists[2].ErrMsg; !strings.Contains(msg, "CANNOT REMOVE LAST") {
		t.Errorf("errMsg = %q", msg)
	}
	if members, _ := f.store.ListUsersInGroup(ctx, ids["zzadmin"]); len(members) != 1 {
		t.Errorf("ZZADMIN members = %+v, want just root", members)
	}
}

// TestAdminSelfRemoveAdminGroupBlockedUserGroups: self-removal from ZZADMIN is
// blocked via the user-groups screen even when ≥2 admins exist (the case
// guardLastAdmin would otherwise pass).
func TestAdminSelfRemoveAdminGroupBlockedUserGroups(t *testing.T) {
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{
			{Cmd: 'G', Row: 1}, // users list: G on root (row 1, alice is row 0)
			{Cmd: 'R', Row: 1}, // groups: remove ZZADMIN (row 1, ops is row 0) — blocked
			{PF: 3}, {PF: 3},
		},
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	f.store.AddUserToGroup(ctx, ids["alice"], ids["zzadmin"]) // 2 admins now
	f.Run(ctx, nil)
	if msg := p.gotLists[2].ErrMsg; msg != "CANNOT REMOVE YOUR OWN ADMIN MEMBERSHIP" {
		t.Errorf("errMsg = %q", msg)
	}
	got, _ := f.store.GetUserGroups(ctx, ids["root"])
	if len(got) == 0 || got[0] != store.AdminGroup {
		t.Errorf("root should remain in ZZADMIN: %v", got)
	}
}

// TestAdminRemoveOtherAdminAllowedUserGroups: removing a *different* admin from
// ZZADMIN is still allowed when ≥2 admins exist.
func TestAdminRemoveOtherAdminAllowedUserGroups(t *testing.T) {
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{
			{Cmd: 'G', Row: 0}, // users list: G on alice (row 0)
			{Cmd: 'R', Row: 1}, // groups: remove ZZADMIN (row 1, ops is row 0) from alice — allowed
			{PF: 3}, {PF: 3},
		},
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	f.store.AddUserToGroup(ctx, ids["alice"], ids["zzadmin"]) // 2 admins now
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if msg := p.gotLists[2].ErrMsg; msg != "" {
		t.Errorf("unexpected errMsg = %q", msg)
	}
	got, _ := f.store.GetUserGroups(ctx, ids["alice"])
	for _, g := range got {
		if g == store.AdminGroup {
			t.Errorf("alice should no longer be in ZZADMIN: %v", got)
		}
	}
}

// TestAdminSelfRemoveNonAdminGroupAllowed: self-removal from a non-ZZADMIN
// group is unaffected by the guardrail.
func TestAdminSelfRemoveNonAdminGroupAllowed(t *testing.T) {
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 1}, {back: true}},
		lists: []ui3270.ListAction{
			{Cmd: 'G', Row: 1}, // users list: G on root (row 1)
			{Cmd: 'R', Row: 0}, // groups: remove OPS (row 0, zzadmin is row 1) from root — allowed
			{PF: 3}, {PF: 3},
		},
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	f.store.AddUserToGroup(ctx, ids["root"], ids["ops"]) // root also in ops
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if msg := p.gotLists[2].ErrMsg; msg != "" {
		t.Errorf("unexpected errMsg = %q", msg)
	}
	got, _ := f.store.GetUserGroups(ctx, ids["root"])
	for _, g := range got {
		if g == "OPS" {
			t.Errorf("root should no longer be in OPS: %v", got)
		}
	}
}

// TestAdminSelfRemoveAdminGroupBlockedGroupMembers: self-removal from ZZADMIN
// is blocked via the group-members screen even when ≥2 admins exist.
func TestAdminSelfRemoveAdminGroupBlockedGroupMembers(t *testing.T) {
	// user rows ALICE(0), ROOT(1); group rows OPS(0), ZZADMIN(1).
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 2}, {back: true}},
		lists: []ui3270.ListAction{
			{Cmd: 'M', Row: 1}, // groups list: M on ZZADMIN (row 1, ops is row 0)
			{Cmd: 'R', Row: 1}, // members: remove ROOT (row 1) — blocked
			{PF: 3}, {PF: 3},
		},
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	f.store.AddUserToGroup(ctx, ids["alice"], ids["zzadmin"]) // 2 admins now
	f.Run(ctx, nil)
	if msg := p.gotLists[2].ErrMsg; msg != "CANNOT REMOVE YOUR OWN ADMIN MEMBERSHIP" {
		t.Errorf("errMsg = %q", msg)
	}
	members, _ := f.store.ListUsersInGroup(ctx, ids["zzadmin"])
	for _, m := range members {
		if m.Username == "ROOT" {
			return // root is still there — good
		}
	}
	t.Error("root should remain in ZZADMIN")
}

// TestAdminRemoveOtherAdminAllowedGroupMembers: removing a *different* admin
// from ZZADMIN via the group-members screen is still allowed when ≥2 admins.
func TestAdminRemoveOtherAdminAllowedGroupMembers(t *testing.T) {
	// user rows ALICE(0), ROOT(1); group rows OPS(0), ZZADMIN(1).
	p := &fakeAdminPresenter{
		menu: []adminMenuStep{{choice: 2}, {back: true}},
		lists: []ui3270.ListAction{
			{Cmd: 'M', Row: 1}, // groups list: M on ZZADMIN (row 1, ops is row 0)
			{Cmd: 'R', Row: 0}, // members: remove ALICE (row 0) — allowed
			{PF: 3}, {PF: 3},
		},
	}
	f, ids := newAdminFixture(t, p)
	ctx := context.Background()
	f.store.AddUserToGroup(ctx, ids["alice"], ids["zzadmin"]) // 2 admins now
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if msg := p.gotLists[2].ErrMsg; msg != "" {
		t.Errorf("unexpected errMsg = %q", msg)
	}
	members, _ := f.store.ListUsersInGroup(ctx, ids["zzadmin"])
	for _, m := range members {
		if m.Username == "ALICE" {
			t.Errorf("ALICE should no longer be in ZZADMIN: %+v", members)
		}
	}
}

func TestAdminAuditUserCreate(t *testing.T) {
	p := &fakeAdminPresenter{forms: []ui3270.FormAction{{Values: map[string]string{
		screens.FieldUsername: "newbie",
		screens.FieldPassword: "pw",
		screens.FieldRetype:   "pw",
	}}}}
	f, _ := newAdminFixture(t, p)
	var got []store.AuditEvent
	f.audit = func(_ context.Context, ev store.AuditEvent) { got = append(got, ev) }

	if err := f.userEdit(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != store.AuditAdmin ||
		got[0].Detail != "user create newbie" {
		t.Errorf("audit = %+v, want one admin 'user create newbie' event", got)
	}
	// GH #73: generic CRUD subject lives in Detail; Username must be empty.
	if got[0].Username != "" {
		t.Errorf("audit Username = %q, want empty (subject described in Detail)", got[0].Username)
	}
}

func TestAdminAuditValidationFailureRecordsNothing(t *testing.T) {
	// A rejected form (duplicate user) must not produce an audit event.
	p := &fakeAdminPresenter{forms: []ui3270.FormAction{
		{Values: map[string]string{
			screens.FieldUsername: "alice", // already exists in the fixture
			screens.FieldPassword: "pw",
			screens.FieldRetype:   "pw",
		}},
		{Cancel: true},
	}}
	f, _ := newAdminFixture(t, p)
	var got []store.AuditEvent
	f.audit = func(_ context.Context, ev store.AuditEvent) { got = append(got, ev) }

	if err := f.userEdit(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("audit = %+v, want no events for a rejected create", got)
	}
}

func TestServiceFormPersistsDescription(t *testing.T) {
	// Drive the ADD-service path and assert the description is stored.
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 3}, {back: true}},
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{{Values: map[string]string{
			screens.FieldName:        "prodcics",
			screens.FieldDescription: "Production CICS",
			screens.FieldHost:        "h",
			screens.FieldPort:        "23",
			screens.FieldTLS:         "N",
			screens.FieldVerify:      "Y",
		}}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	svcs, err := f.store.ListAllServices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found *store.Service
	for i := range svcs {
		if svcs[i].Name == "PRODCICS" {
			found = &svcs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("PRODCICS not found in services: %+v", svcs)
	}
	if found.Description != "Production CICS" {
		t.Errorf("Description = %q, want %q", found.Description, "Production CICS")
	}
}

// --- Trusted Networks admin flow tests ---

func TestAdminNetworkAddHappyPath(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 5}, {back: true}},
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{{Values: map[string]string{
			screens.FieldCIDR:    "10.0.0.0/24",
			screens.FieldComment: "internal OEC",
		}}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	nets, err := f.store.ListTrustedNetworks(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(nets) != 1 || nets[0].CIDR != "10.0.0.0/24" || nets[0].Comment != "internal OEC" {
		t.Errorf("networks = %+v", nets)
	}
}

func TestAdminNetworkAddBareIP(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 5}, {back: true}},
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{{Values: map[string]string{
			screens.FieldCIDR:    "192.168.1.5",
			screens.FieldComment: "dev workstation",
		}}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	nets, _ := f.store.ListTrustedNetworks(ctx)
	if len(nets) != 1 || nets[0].CIDR != "192.168.1.5/32" {
		t.Errorf("stored CIDR = %q, want 192.168.1.5/32", nets[0].CIDR)
	}
}

func TestAdminNetworkAddBadCIDRShowsError(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 5}, {back: true}},
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{
			{Values: map[string]string{screens.FieldCIDR: "not-an-ip", screens.FieldComment: "ok"}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	f.Run(ctx, nil)
	if msg := p.gotForms[1].ErrMsg; msg == "" {
		t.Error("expected error for bad CIDR, got empty")
	}
	nets, _ := f.store.ListTrustedNetworks(ctx)
	if len(nets) != 0 {
		t.Errorf("no network should be stored: %+v", nets)
	}
}

func TestAdminNetworkAddEmptyCommentShowsError(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 5}, {back: true}},
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{
			{Values: map[string]string{screens.FieldCIDR: "10.0.0.0/24", screens.FieldComment: ""}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	f.Run(ctx, nil)
	if msg := p.gotForms[1].ErrMsg; msg == "" {
		t.Error("expected error for empty comment, got empty")
	}
}

func TestAdminNetworkFormPreservesInputOnError(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 5}, {back: true}},
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}},
		forms: []ui3270.FormAction{
			{Values: map[string]string{screens.FieldCIDR: "bad", screens.FieldComment: "my net"}},
			{Cancel: true},
		},
	}
	f, _ := newAdminFixture(t, p)
	f.Run(context.Background(), nil)
	last := p.gotForms[len(p.gotForms)-1]
	if last.Fields[0].Value != "bad" {
		t.Errorf("CIDR not re-seeded: %q", last.Fields[0].Value)
	}
	if last.Fields[1].Value != "my net" {
		t.Errorf("comment not re-seeded: %q", last.Fields[1].Value)
	}
}

func TestAdminNetworkEdit(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 5}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}},
		forms: []ui3270.FormAction{{Values: map[string]string{
			screens.FieldCIDR: "10.0.1.0/24", screens.FieldComment: "updated",
		}}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	// Pre-seed a network so the list has row 0 to edit.
	if _, err := f.store.CreateTrustedNetwork(ctx, "10.0.0.0/24", "original"); err != nil {
		t.Fatal(err)
	}
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	nets, _ := f.store.ListTrustedNetworks(ctx)
	if len(nets) != 1 || nets[0].CIDR != "10.0.1.0/24" || nets[0].Comment != "updated" {
		t.Errorf("after edit: %+v", nets)
	}
	// The edit form must be pre-filled with the existing CIDR.
	if got := p.gotForms[0].Fields[0].Value; got != "10.0.0.0/24" {
		t.Errorf("edit pre-fill CIDR = %q, want 10.0.0.0/24", got)
	}
}

func TestAdminNetworkDeleteConfirm(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 5}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'D', Row: 0}, {}, {PF: 3}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	// Pre-seed a network to delete.
	if _, err := f.store.CreateTrustedNetwork(ctx, "10.0.0.0/24", "to delete"); err != nil {
		t.Fatal(err)
	}
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	// gotLists[1] is the re-render after D: it carries the confirm prompt.
	if msg := p.gotLists[1].ErrMsg; !strings.Contains(msg, "CONFIRM DELETE OF '10.0.0.0/24'") {
		t.Errorf("confirm prompt = %q", msg)
	}
	nets, _ := f.store.ListTrustedNetworks(ctx)
	if len(nets) != 0 {
		t.Errorf("network should be deleted: %+v", nets)
	}
}

func TestAdminNetworkDeleteCancel(t *testing.T) {
	p := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 5}, {back: true}},
		lists: []ui3270.ListAction{{Cmd: 'D', Row: 0}, {PF: 3}, {PF: 3}},
	}
	f, _ := newAdminFixture(t, p)
	ctx := context.Background()
	// Pre-seed a network; delete should be cancelled.
	if _, err := f.store.CreateTrustedNetwork(ctx, "10.0.0.0/24", "keep me"); err != nil {
		t.Fatal(err)
	}
	if err := f.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	nets, _ := f.store.ListTrustedNetworks(ctx)
	if len(nets) != 1 {
		t.Errorf("network should survive cancel: %+v", nets)
	}
}

func TestAdminNetworkAudit(t *testing.T) {
	p := &fakeAdminPresenter{
		forms: []ui3270.FormAction{{Values: map[string]string{
			screens.FieldCIDR:    "10.0.0.0/24",
			screens.FieldComment: "internal OEC",
		}}},
	}
	f, _ := newAdminFixture(t, p)
	var got []store.AuditEvent
	f.audit = func(_ context.Context, ev store.AuditEvent) { got = append(got, ev) }
	if err := f.networkForm(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != store.AuditAdmin ||
		!strings.Contains(got[0].Detail, "trust create") {
		t.Errorf("audit = %+v", got)
	}
}

func TestAdminAuditActorSubjectSplit(t *testing.T) {
	var got []store.AuditEvent
	// auditFn simulates the auditTrail's auto-fill: the acting admin is the
	// session principal, stamped onto any actor-less event.
	const admin = "ADMIN"
	auditFn := func(_ context.Context, ev store.AuditEvent) {
		if ev.Actor == "" {
			ev.Actor = admin
		}
		got = append(got, ev)
	}

	// Generic CRUD: recordAdmin must NOT put the admin in Username.
	f := &adminFlow{identity: auth.Identity{Username: admin}, audit: auditFn}
	f.recordAdmin(context.Background(), "created service PROD")

	if len(got) != 1 {
		t.Fatalf("recordAdmin emitted %d events, want 1", len(got))
	}
	if got[0].Username != "" {
		t.Errorf("generic CRUD username = %q, want empty (subject lives in Detail)", got[0].Username)
	}
	if got[0].Actor != admin {
		t.Errorf("generic CRUD actor = %q, want %q", got[0].Actor, admin)
	}
	if got[0].Detail != "created service PROD" {
		t.Errorf("generic CRUD detail = %q, want the change description", got[0].Detail)
	}
}

// TestSessionAdminOption7ThreadsRegistry verifies that when Session.Registry is
// set, the live-session registry (and selfSessionID) are threaded into the
// adminFlow so that option 7 (Active Sessions) renders the registry's sessions
// and marks the admin's own row *YOU*.
func TestSessionAdminOption7ThreadsRegistry(t *testing.T) {
	// Register the admin's session in a real registry.
	reg := newSessionRegistry()
	selfID := reg.register("127.0.0.1:9999", time.Now(), func() {})
	reg.setLogin(selfID, "ROOT", time.Now())

	// sessRenderer scripted to immediately PF3 back from the sessions screen.
	sr := &sessRenderer{acts: []ui3270.ListAction{{PF: 3}}}

	// fakePresenter: login as root, pick menuAdmin once, then quit.
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "root", pass: "good"},
			{quit: true},
		},
		menuPicks: []menuResult{{choice: menuAdmin}, {quit: true}},
	}

	// fakeAdminPresenter: choose option 7 once, then back.
	ap := &fakeAdminPresenter{menu: []adminMenuStep{{choice: 7}, {back: true}}}

	s := newTestSession(t, p, &fakeBridger{})
	s.AdminPresenter = ap
	s.AdminRenderer = func(_ net.Conn, _ Term) ui3270.Renderer { return sr }
	s.Registry = reg
	s.SessionID = selfID

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	s.Run(client)

	// After the fix: the sessRenderer must have been called exactly once
	// (activeSessions ran and rendered) and the row for the admin must say *YOU*.
	if len(sr.views) != 1 {
		t.Fatalf("sessRenderer views = %d, want 1 (registry was not threaded into adminFlow)", len(sr.views))
	}
	rows := sr.views[0].Rows
	if len(rows) != 1 {
		t.Fatalf("snapshot rows = %d, want 1", len(rows))
	}
	if !strings.Contains(rows[0].Left, "*YOU*") {
		t.Errorf("admin row = %q, want *YOU* (selfSessionID was not threaded)", rows[0].Left)
	}
}
