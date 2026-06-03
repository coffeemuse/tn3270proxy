package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// newTestStore opens a fresh migrated store in a temp directory.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestMigrateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idem.db")
	st1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	st1.Close()
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open (re-migrate) failed: %v", err)
	}
	st2.Close()
}

func TestMigrateAddsVerifyToLegacyDB(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	// Simulate a pre-tls_verify database: services table WITHOUT the column.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`CREATE TABLE services (
		id   INTEGER PRIMARY KEY,
		name TEXT UNIQUE NOT NULL,
		host TEXT NOT NULL,
		port INTEGER NOT NULL,
		tls  INTEGER NOT NULL DEFAULT 0
	);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	// Open through the store: migrate() must ALTER in tls_verify (default 1).
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy db: %v", err)
	}
	defer st.Close()

	ops, _ := st.CreateGroup(ctx, "ops")
	sid, _ := st.CreateService(ctx, "SEC", "sec.example", 992, true, true)
	st.LinkGroupService(ctx, ops, sid)

	svcs, err := st.ListServicesForGroups(ctx, []string{"ops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 1 || !svcs[0].TLSVerify {
		t.Fatalf("legacy migration: want TLSVerify true, got %+v", svcs)
	}
}

func TestOpenCreatesTables(t *testing.T) {
	st := newTestStore(t)
	want := []string{"users", "groups", "user_groups", "services", "group_services"}
	for _, table := range want {
		var name string
		err := st.db.QueryRowContext(context.Background(),
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}
