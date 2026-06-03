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

// seedTriangle creates user alice in group ops with service PROD linked to ops,
// returning the three ids.
func seedTriangle(t *testing.T, st *Store) (uid, gid, sid int64) {
	t.Helper()
	ctx := context.Background()
	uid, _ = st.CreateUser(ctx, "alice", "h")
	gid, _ = st.CreateGroup(ctx, "ops")
	sid, _ = st.CreateService(ctx, "PROD", "h", 23, false, true)
	st.AddUserToGroup(ctx, uid, gid)
	st.LinkGroupService(ctx, gid, sid)
	return uid, gid, sid
}

func countTableRows(t *testing.T, st *Store, table, where string, arg int64) int {
	t.Helper()
	var n int
	if err := st.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table+" WHERE "+where, arg).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDeleteUserCascadesMemberships(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	uid, gid, _ := seedTriangle(t, st)
	if err := st.DeleteUser(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetUserByUsername(ctx, "alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("user still present: %v", err)
	}
	if n := countTableRows(t, st, "user_groups", "user_id = ?", uid); n != 0 {
		t.Errorf("memberships left = %d", n)
	}
	// the group survives
	if n := countTableRows(t, st, "groups", "id = ?", gid); n != 1 {
		t.Errorf("group rows = %d, want 1", n)
	}
}

func TestDeleteGroupCascadesLinksAndMemberships(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	uid, gid, sid := seedTriangle(t, st)
	if err := st.DeleteGroup(ctx, gid); err != nil {
		t.Fatal(err)
	}
	if n := countTableRows(t, st, "user_groups", "group_id = ?", gid); n != 0 {
		t.Errorf("memberships left = %d", n)
	}
	if n := countTableRows(t, st, "group_services", "group_id = ?", gid); n != 0 {
		t.Errorf("links left = %d", n)
	}
	// user and service survive
	if n := countTableRows(t, st, "users", "id = ?", uid); n != 1 {
		t.Errorf("user rows = %d", n)
	}
	if n := countTableRows(t, st, "services", "id = ?", sid); n != 1 {
		t.Errorf("service rows = %d", n)
	}
}

func TestDeleteServiceCascadesLinks(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	_, _, sid := seedTriangle(t, st)
	if err := st.DeleteService(ctx, sid); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetService(ctx, sid); !errors.Is(err, ErrNotFound) {
		t.Errorf("service still present: %v", err)
	}
	if n := countTableRows(t, st, "group_services", "service_id = ?", sid); n != 0 {
		t.Errorf("links left = %d", n)
	}
}

func TestRemoveUserFromGroupAndUnlink(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	uid, gid, sid := seedTriangle(t, st)
	if err := st.RemoveUserFromGroup(ctx, uid, gid); err != nil {
		t.Fatal(err)
	}
	if n := countTableRows(t, st, "user_groups", "user_id = ?", uid); n != 0 {
		t.Errorf("membership left")
	}
	if err := st.UnlinkGroupService(ctx, gid, sid); err != nil {
		t.Fatal(err)
	}
	if n := countTableRows(t, st, "group_services", "group_id = ?", gid); n != 0 {
		t.Errorf("link left")
	}
	// removing again is a silent no-op (idempotent, mirrors Add/Link)
	if err := st.RemoveUserFromGroup(ctx, uid, gid); err != nil {
		t.Errorf("re-remove err = %v", err)
	}
}

func TestGroupCounts(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	_, gid, _ := seedTriangle(t, st)
	if n, err := st.CountGroupMembers(ctx, gid); err != nil || n != 1 {
		t.Errorf("members = %d, %v; want 1", n, err)
	}
	if n, err := st.CountGroupServices(ctx, gid); err != nil || n != 1 {
		t.Errorf("services = %d, %v; want 1", n, err)
	}
	if n, err := st.CountGroupMembers(ctx, 99999); err != nil || n != 0 {
		t.Errorf("missing group members = %d, %v; want 0", n, err)
	}
}

func TestListUsersInGroup(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	uid, gid, _ := seedTriangle(t, st) // alice in ops
	zoe, _ := st.CreateUser(ctx, "zoe", "h")
	st.CreateUser(ctx, "bob", "h") // NOT in ops — must not appear
	st.AddUserToGroup(ctx, zoe, gid)

	users, err := st.ListUsersInGroup(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0].Username != "alice" || users[1].Username != "zoe" {
		t.Fatalf("users = %+v, want [alice zoe] (ordered by username)", users)
	}
	if users[0].ID != uid {
		t.Errorf("alice ID = %d, want %d", users[0].ID, uid)
	}
	// missing/empty group → empty result, no error
	if empty, err := st.ListUsersInGroup(ctx, 99999); err != nil || len(empty) != 0 {
		t.Errorf("missing group = %+v, %v; want empty, nil", empty, err)
	}
}
