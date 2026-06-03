package store

import (
	"context"
	"errors"
	"testing"
)

func TestMigrateCreatesAdminGroup(t *testing.T) {
	st := newTestStore(t)
	var n int
	err := st.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM groups WHERE name = ?", AdminGroup).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("ZZADMIN rows = %d, want 1", n)
	}
	// Re-running migrate must stay idempotent (no duplicate, no error).
	if err := st.migrate(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if err := st.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM groups WHERE name = ?", AdminGroup).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("after re-migrate ZZADMIN rows = %d, want 1", n)
	}
}

func TestAdminGroupIsReserved(t *testing.T) {
	if AdminGroup != "ZZADMIN" || ReservedGroupPrefix != "ZZ" {
		t.Fatalf("constants = %q/%q", AdminGroup, ReservedGroupPrefix)
	}
}

func TestSessionUsesAdminGroupConstant(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	// the auto-created group is usable like any other
	gid, err := st.CreateGroup(ctx, AdminGroup) // idempotent: returns existing id
	if err != nil || gid == 0 {
		t.Fatalf("CreateGroup(%s) = %d, %v", AdminGroup, gid, err)
	}
}

func TestListUsersOrdered(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	st.CreateUser(ctx, "zoe", "h1")
	st.CreateUser(ctx, "abe", "h2")
	users, err := st.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0].Username != "abe" || users[1].Username != "zoe" {
		t.Fatalf("users = %+v", users)
	}
}

func TestListGroupsOrderedIncludesAdmin(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	st.CreateGroup(ctx, "ops")
	groups, err := st.ListGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// ORDER BY name is binary: uppercase sorts before lowercase, so
	// ZZADMIN (auto-created by migrate) comes before ops.
	if len(groups) != 2 || groups[0].Name != AdminGroup || groups[1].Name != "ops" {
		t.Fatalf("groups = %+v", groups)
	}
	if groups[0].ID == 0 {
		t.Errorf("group ID not populated")
	}
}

func TestListAllServicesAndGetService(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	id, _ := st.CreateService(ctx, "PROD", "h1", 23, true, false)
	st.CreateService(ctx, "DEV", "h2", 992, false, true)
	svcs, err := st.ListAllServices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 2 || svcs[0].Name != "DEV" || svcs[1].Name != "PROD" {
		t.Fatalf("services = %+v", svcs)
	}
	if !svcs[1].TLS || svcs[1].TLSVerify {
		t.Errorf("PROD TLS flags = %v/%v, want true/false", svcs[1].TLS, svcs[1].TLSVerify)
	}
	got, err := st.GetService(ctx, id)
	if err != nil || got.Name != "PROD" || got.Host != "h1" || got.Port != 23 {
		t.Fatalf("GetService = %+v, %v", got, err)
	}
	if _, err := st.GetService(ctx, 99999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing service err = %v, want ErrNotFound", err)
	}
}

func TestListGroupsForService(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	gid, _ := st.CreateGroup(ctx, "ops")
	sid, _ := st.CreateService(ctx, "PROD", "h", 23, false, true)
	st.LinkGroupService(ctx, gid, sid)
	groups, err := st.ListGroupsForService(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Name != "ops" || groups[0].ID != gid {
		t.Fatalf("groups = %+v", groups)
	}
}

func TestSetPassword(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	uid, _ := st.CreateUser(ctx, "alice", "oldhash")
	if err := st.SetPassword(ctx, uid, "newhash"); err != nil {
		t.Fatal(err)
	}
	u, err := st.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if u.PasswordHash != "newhash" {
		t.Errorf("hash = %q, want newhash", u.PasswordHash)
	}
	if err := st.SetPassword(ctx, 99999, "h"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing user err = %v, want ErrNotFound", err)
	}
}

func TestUpdateService(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	sid, _ := st.CreateService(ctx, "PROD", "h1", 23, false, true)
	if err := st.UpdateService(ctx, sid, "PROD2", "h2", 992, true, false); err != nil {
		t.Fatal(err)
	}
	svc, err := st.GetService(ctx, sid)
	if err != nil {
		t.Fatalf("GetService: %v", err)
	}
	want := Service{ID: sid, Name: "PROD2", Host: "h2", Port: 992, TLS: true, TLSVerify: false}
	if svc != want {
		t.Errorf("service = %+v, want %+v", svc, want)
	}
	if err := st.UpdateService(ctx, 99999, "X", "h", 23, false, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing service err = %v, want ErrNotFound", err)
	}
}
