package server

import (
	"context"
	"net"
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

func TestAdminStoreSatisfiedByStore(t *testing.T) {
	var _ AdminStore = (*store.Store)(nil)
}
