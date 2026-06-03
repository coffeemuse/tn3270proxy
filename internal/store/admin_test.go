package store

import (
	"context"
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
