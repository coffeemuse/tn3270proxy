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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreshDBStampsLatestVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	var v int
	if err := st.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if v != maxKnownVersion() {
		t.Fatalf("user_version = %d, want %d", v, maxKnownVersion())
	}
}

func TestReconcileDefaultsSeedsAdminGroup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "seed.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	var n int
	if err := st.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM groups WHERE name = ?", AdminGroup).Scan(&n); err != nil {
		t.Fatalf("count admin group: %v", err)
	}
	if n != 1 {
		t.Fatalf("admin group rows = %d, want 1", n)
	}
}

func TestForeignKeyCheckCleanAfterMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fk.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	rows, err := st.db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign_key_check reported violations after migrate")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("foreign_key_check iteration: %v", err)
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newer.db")

	// First open creates a current DB.
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Stamp a version one beyond what this build knows.
	if _, err := st.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", maxKnownVersion()+1)); err != nil {
		t.Fatalf("bump user_version: %v", err)
	}
	st.Close()

	// Reopen: must refuse.
	_, err = Open(path)
	if !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("Open newer db: err = %v, want ErrSchemaNewer", err)
	}
}

// legacyServicesDB creates a pre-tls_verify services-only database (user_version 0)
// with one row, mimicking a real legacy DB that needs migrating.
func legacyServicesDB(t *testing.T, path string) {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`CREATE TABLE services (
		id   INTEGER PRIMARY KEY,
		name TEXT UNIQUE NOT NULL,
		host TEXT NOT NULL,
		port INTEGER NOT NULL,
		tls  INTEGER NOT NULL DEFAULT 0
	);`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		"INSERT INTO services (name, host, port) VALUES ('OLD','old.example',23)"); err != nil {
		t.Fatal(err)
	}
}

func bakFiles(t *testing.T, dbPath string) []string {
	t.Helper()
	matches, err := filepath.Glob(dbPath + ".pre-migrate-*.bak")
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestMigrationBacksUpLegacyDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacyServicesDB(t, path)

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	baks := bakFiles(t, path)
	if len(baks) != 1 {
		t.Fatalf("got %d backup files, want 1: %v", len(baks), baks)
	}

	// The backup is a real pre-migration snapshot: it still has the OLD row and
	// (being v0) lacks tls_verify, proving it predates the migration.
	snap, err := sql.Open("sqlite", baks[0])
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer snap.Close()
	var n int
	if err := snap.QueryRow("SELECT COUNT(*) FROM services WHERE name='OLD'").Scan(&n); err != nil {
		t.Fatalf("query backup: %v", err)
	}
	if n != 1 {
		t.Fatalf("backup OLD rows = %d, want 1", n)
	}
	// The snapshot predates the migration: the v0 services table lacks tls_verify.
	var dummy int
	if err := snap.QueryRow("SELECT tls_verify FROM services LIMIT 1").Scan(&dummy); err == nil {
		t.Fatal("backup has tls_verify column — it was taken post-migration, not before")
	}
}

func TestFreshDBMakesNoBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh-nobak.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	if baks := bakFiles(t, path); len(baks) != 0 {
		t.Fatalf("fresh DB created backups: %v", baks)
	}
}

// colInfo is the order-independent shape of a column (cid is intentionally
// excluded: ALTER ADD COLUMN appends, so positions differ from a fresh CREATE).
type colInfo struct {
	ctype   string
	notnull int
	dflt    string
	pk      int
}

func columnSet(t *testing.T, db *sql.DB, table string) map[string]colInfo {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	defer rows.Close()
	out := map[string]colInfo{}
	for rows.Next() {
		var (
			cid  int
			name string
			ci   colInfo
			dflt sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ci.ctype, &ci.notnull, &dflt, &ci.pk); err != nil {
			t.Fatalf("scan table_info(%s): %v", table, err)
		}
		ci.dflt = dflt.String
		out[name] = ci
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	return out
}

func tableNames(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(
		"SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list tables: %v", err)
	}
	return out
}

// TestLegacyConvergesToFreshSchema is the headline guarantee: a migrated legacy
// DB has the same tables and the same per-column shape (type/notnull/default/pk)
// as a freshly created one. Column ORDER and COLLATE are not compared — additive
// ALTER cannot reproduce them, and they do not affect correctness here.
func TestLegacyConvergesToFreshSchema(t *testing.T) {
	freshPath := filepath.Join(t.TempDir(), "fresh.db")
	fresh, err := Open(freshPath)
	if err != nil {
		t.Fatalf("Open fresh: %v", err)
	}
	defer fresh.Close()

	legacyPath := filepath.Join(t.TempDir(), "legacy.db")
	legacyServicesDB(t, legacyPath) // user_version 0, services-only, missing columns
	migrated, err := Open(legacyPath)
	if err != nil {
		t.Fatalf("Open legacy: %v", err)
	}
	defer migrated.Close()

	freshTables := tableNames(t, fresh.db)
	if got := tableNames(t, migrated.db); !slicesEqual(got, freshTables) {
		t.Fatalf("tables differ:\n fresh    = %v\n migrated = %v", freshTables, got)
	}
	for _, tbl := range freshTables {
		want := columnSet(t, fresh.db, tbl)
		got := columnSet(t, migrated.db, tbl)
		if len(want) != len(got) {
			t.Fatalf("%s: column count fresh=%d migrated=%d", tbl, len(want), len(got))
		}
		for col, w := range want {
			g, ok := got[col]
			if !ok {
				t.Fatalf("%s: migrated missing column %q", tbl, col)
			}
			if g != w {
				t.Fatalf("%s.%s: fresh=%+v migrated=%+v", tbl, col, w, g)
			}
		}
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// buildV2DB writes a v2-schema database by hand (baseline schema + audit.actor
// + user_version=2) so Open exercises exactly the v2→v3 step.
func buildV2DB(t *testing.T, dbPath string, configRows map[string]string) {
	t.Helper()
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("ALTER TABLE audit ADD COLUMN actor TEXT NOT NULL DEFAULT ''"); err != nil {
		t.Fatal(err)
	}
	for k, v := range configRows {
		if _, err := raw.Exec("INSERT INTO system_config (key, value) VALUES (?, ?)", k, v); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.Exec("PRAGMA user_version = 2"); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateV3ImportsConfiguredFiles(t *testing.T) {
	dir := t.TempDir()
	motd := filepath.Join(dir, "motd.txt")
	if err := os.WriteFile(motd, []byte("HELLO\nWORLD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "test.db")
	buildV2DB(t, dbPath, map[string]string{
		"MOTD_FILE":     motd,
		"BRANDING_FILE": filepath.Join(dir, "missing.txt"), // unreadable → non-fatal skip
	})

	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	d, err := st.GetDocument(context.Background(), DocMOTD)
	if err != nil {
		t.Fatal(err)
	}
	if d.Content != "HELLO\nWORLD\n" || d.UpdatedBy != "migration" {
		t.Errorf("MOTD after migration: %+v", d)
	}
	b, err := st.GetDocument(context.Background(), DocBranding)
	if err != nil {
		t.Fatal(err)
	}
	if b.Content != "" {
		t.Errorf("BRANDING should be empty after unreadable-file skip, got %q", b.Content)
	}
}

func TestMigrateV3TruncatesOversizeFile(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", MaxDocumentBytes+100)), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "test.db")
	buildV2DB(t, dbPath, map[string]string{"MOTD_FILE": big})

	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d, err := st.GetDocument(context.Background(), DocMOTD)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Content) != MaxDocumentBytes {
		t.Errorf("oversize import: len=%d, want truncated to %d", len(d.Content), MaxDocumentBytes)
	}
}

// legacyAuditDB creates a user_version=0 DB whose audit table predates the actor
// column (GH #73), with one row, mimicking a real pre-v2 DB that needs migrating.
func legacyAuditDB(t *testing.T, path string) {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`CREATE TABLE audit (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		at          TEXT NOT NULL,
		session_id  TEXT NOT NULL,
		kind        TEXT NOT NULL,
		username    TEXT NOT NULL DEFAULT '',
		remote_addr TEXT NOT NULL DEFAULT '',
		service     TEXT NOT NULL DEFAULT '',
		detail      TEXT NOT NULL DEFAULT ''
	);`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		`INSERT INTO audit (at, session_id, kind, username)
		 VALUES ('2026-06-01T00:00:00Z','s0','mfa_cleared','BOB')`); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationAddsAndBackfillsAuditActor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-audit.db")
	legacyAuditDB(t, path)

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	if _, ok := columnSet(t, st.db, "audit")["actor"]; !ok {
		t.Fatal("audit.actor column missing after migrate")
	}
	var actor string
	if err := st.db.QueryRow(
		"SELECT actor FROM audit WHERE username='BOB'").Scan(&actor); err != nil {
		t.Fatalf("query actor: %v", err)
	}
	if actor != "BOB" {
		t.Errorf("backfilled actor = %q, want BOB", actor)
	}
	var n int
	if err := st.db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='audit_actor'").Scan(&n); err != nil {
		t.Fatalf("query index: %v", err)
	}
	if n != 1 {
		t.Errorf("audit_actor index count = %d, want 1", n)
	}
}

// A DB that predates the HELP-MENU entry gains the stock seed on Open via
// reconcileDefaults — no migration step involved.
func TestExistingDBGainsHelpMenuDocument(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	buildV2DB(t, dbPath, map[string]string{})
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d, err := st.GetDocument(context.Background(), DocHelpMenu)
	if err != nil {
		t.Fatal(err)
	}
	if d.Content != DefaultHelpMenuContent {
		t.Errorf("pre-existing DB should gain the stock HELP-MENU on open, got %q", d.Content)
	}
}

// TestSecondOpenIsNoOp confirms reopening a current DB neither changes the
// version nor writes a new backup.
func TestSecondOpenIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twice.db")
	st1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	st1.Close()

	st2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer st2.Close()

	var v int
	if err := st2.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if v != maxKnownVersion() {
		t.Fatalf("user_version = %d, want %d", v, maxKnownVersion())
	}
	if baks := bakFiles(t, path); len(baks) != 0 {
		t.Fatalf("no-op reopen wrote backups: %v", baks)
	}
}
