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
	sid, _ := st.CreateService(ctx, "SEC", "Secure Host", "sec.example", 992, true, true)
	st.LinkGroupService(ctx, ops, sid)

	svcs, err := st.ListServicesForGroups(ctx, []string{"ops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 1 || !svcs[0].TLSVerify {
		t.Fatalf("legacy migration: want TLSVerify true, got %+v", svcs)
	}
}

func TestMigrateAddsDescriptionToLegacyDB(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy_desc.db")

	// Simulate a pre-description database: services table WITHOUT the column.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`CREATE TABLE services (
		id   INTEGER PRIMARY KEY,
		name TEXT UNIQUE NOT NULL,
		host TEXT NOT NULL,
		port INTEGER NOT NULL,
		tls  INTEGER NOT NULL DEFAULT 0,
		tls_verify INTEGER NOT NULL DEFAULT 1
	);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	// Open through the store: migrate() must ALTER in description (default '').
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy db: %v", err)
	}
	defer st.Close()

	ops, _ := st.CreateGroup(ctx, "ops")
	sid, _ := st.CreateService(ctx, "SEC", "Secure Host", "sec.example", 992, true, true)
	st.LinkGroupService(ctx, ops, sid)

	svcs, err := st.ListServicesForGroups(ctx, []string{"ops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 1 || svcs[0].Description != "Secure Host" {
		t.Fatalf("legacy migration: want Description 'Secure Host', got %+v", svcs)
	}
}

func TestMigrateAddsUserDetailsToLegacyDB(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy_userdetails.db")

	// Simulate a pre-details database: users table WITHOUT full_name/email.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`CREATE TABLE users (
		id            INTEGER PRIMARY KEY,
		username      TEXT UNIQUE COLLATE NOCASE NOT NULL,
		password_hash TEXT NOT NULL
	);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	// Open through the store: migrate() must ALTER in full_name/email (default '').
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy db: %v", err)
	}
	defer st.Close()

	if _, err := st.CreateUser(ctx, "alice", "hash-a"); err != nil {
		t.Fatalf("CreateUser on migrated db: %v", err)
	}
	u, err := st.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if u.FullName != "" || u.Email != "" {
		t.Fatalf("legacy migration: want empty details, got %q/%q", u.FullName, u.Email)
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

func TestUsernameFoldsToUppercase(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if _, err := st.CreateUser(ctx, "alice", "hash"); err != nil {
		t.Fatalf("create: %v", err)
	}
	u, err := st.GetUserByUsername(ctx, "ALICE")
	if err != nil {
		t.Fatalf("lookup ALICE: %v", err)
	}
	if u.Username != "ALICE" {
		t.Errorf("stored username = %q, want ALICE", u.Username)
	}
	if u2, err := st.GetUserByUsername(ctx, "aLiCe"); err != nil || u2.ID != u.ID {
		t.Errorf("aLiCe lookup = (%+v, %v), want same id %d", u2, err, u.ID)
	}
}

func TestGroupNameFoldsToUppercase(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id, err := st.CreateGroup(ctx, "zzadmin")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id2, err := st.CreateGroup(ctx, "ZZADMIN")
	if err != nil || id2 != id {
		t.Fatalf("re-create ZZADMIN = (%d, %v), want same id %d", id2, err, id)
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

func TestNormalizeServiceName(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"prod", "PROD", false},
		{" Tso ", "TSO", false},
		{"PRODCICS", "PRODCICS", false},
		{"prod cics", "", true},
		{"PROD_CICS", "", true},
		{"TOOLONGNM", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeServiceName(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("NormalizeServiceName(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("NormalizeServiceName(%q) = (%q, %v), want (%q, nil)", c.in, got, err, c.want)
		}
	}
}

func TestCreateServiceFoldsNameAndRequiresDescription(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id, err := st.CreateService(ctx, "prod", "Production CICS", "h", 23, false, true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id2, err := st.CreateService(ctx, "PROD", "Production CICS", "h", 23, false, true)
	if err != nil || id2 != id {
		t.Fatalf("dedup create = (%d, %v), want same id %d", id2, err, id)
	}
	svcs, _ := st.ListAllServices(ctx)
	if len(svcs) != 1 || svcs[0].Name != "PROD" || svcs[0].Description != "Production CICS" {
		t.Fatalf("services = %+v, want one PROD/Production CICS", svcs)
	}
	if _, err := st.CreateService(ctx, "TSO", "", "h", 23, false, true); err == nil {
		t.Error("empty description: want error, got nil")
	}
	if _, err := st.CreateService(ctx, "bad name", "desc", "h", 23, false, true); err == nil {
		t.Error("invalid name: want error, got nil")
	}
}
