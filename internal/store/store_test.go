package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

func TestOpenPragmas(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	var journalMode string
	if err := st.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want wal", journalMode)
	}

	var foreignKeys int
	if err := st.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1", foreignKeys)
	}
}

// TestConcurrentAuditInserts fails with "database is locked" before the fix (busy_timeout=0, delete mode).
func TestConcurrentAuditInserts(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	const workers = 20
	at := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			errs <- st.RecordAudit(ctx, AuditEvent{
				At:        at,
				SessionID: fmt.Sprintf("s%d", i),
				Kind:      AuditConnect,
			})
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("concurrent insert: %v", err)
		}
	}

	got, err := st.ListAudit(ctx, AuditFilter{Limit: workers + 1})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(got) != workers {
		t.Errorf("want %d audit rows, got %d", workers, len(got))
	}
}
