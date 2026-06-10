# Schema Migration Framework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the additive-only, version-less `store.migrate()` with a versioned, forward-only migration framework keyed on `PRAGMA user_version`, with an auto-backup before any migration and a fail-closed refusal when the DB is newer than the binary.

**Architecture:** Numbered Go migrations in an append-only ledger run on a single dedicated connection (foreign keys off, integrity re-checked at the end), each in its own transaction that also stamps `user_version`. v1 is the baseline (= today's schema). Code-defined seed rows (ZZADMIN group + sysconfig defaults) move to an idempotent `reconcileDefaults` that runs every boot, *outside* the version ledger. A reusable `Store.Backup` primitive (VACUUM INTO) serves both the runner and a future admin maintenance screen.

**Tech Stack:** Go, `modernc.org/sqlite` (pure-Go), `database/sql`, `log/slog`. Tests with the standard `testing` package; run `go test ./... -race`.

**Spec:** `docs/superpowers/specs/2026-06-08-schema-migration-framework-design.md` ([#88](https://github.com/coffeemuse/TN3270Proxy/issues/88))

---

## File Structure

- **Create `internal/store/migrate.go`** — the migration ledger (`migration` type, `migrations` slice, `migrateV1Baseline`), the runner (`runMigrations`, `runOne`), `reconcileDefaults`, `ensureColumnTx`, `schemaVersion`, `isFresh`, and `ErrSchemaNewer`. The `schema` const moves here from `store.go`.
- **Create `internal/store/maintenance.go`** — reusable DB-maintenance primitives. `Store.Backup` + the shared `vacuumInto` helper now; shaped to host `Vacuum`/`IntegrityCheck`/`SchemaVersion` later (out of scope).
- **Modify `internal/store/store.go`** — add `path` field to `Store`; set it in `Open`; rewrite `migrate()` to call `runMigrations` then `reconcileDefaults`; remove the old `schema` const, old `ensureColumn`, and the old inline ZZADMIN/sysconfig seeding (now in the new files).
- **Create `internal/store/migrate_test.go`** — version stamping, newer-DB guard, FK integrity, convergence, idempotency.
- **Create `internal/store/maintenance_test.go`** — `Backup` snapshot + clobber-refusal.
- **Modify `CLAUDE.md`** — store package-map entry + the seeding-boundary convention.
- **Modify `.gitignore`** — ignore `*.bak`.
- **Create `docs/operations-upgrades.md`** — short operator note: upgrade/backup/rollback.

Existing tests in `internal/store/store_test.go` (`TestMigrateAddsVerifyToLegacyDB`, `TestMigrateAddsDescriptionToLegacyDB`, `TestMigrateAddsUserDetailsToLegacyDB`, and the double-`Open` idempotency check) are the safety net — they must stay green throughout.

---

## Task 1: Migration ledger, runner, and reconcileDefaults

**Files:**
- Create: `internal/store/migrate.go`
- Modify: `internal/store/store.go` (Store struct, Open, migrate; remove old schema/ensureColumn/seeding)
- Test: `internal/store/migrate_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/store/migrate_test.go`:

```go
package store

import (
	"context"
	"path/filepath"
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
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestFreshDBStampsLatestVersion|TestReconcileDefaultsSeedsAdminGroup|TestForeignKeyCheckCleanAfterMigrate' -v`
Expected: compile failure — `maxKnownVersion` undefined (and `reconcileDefaults` not yet wired).

- [ ] **Step 3: Create `internal/store/migrate.go`**

```go
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
	"log/slog"

	"github.com/coffeemuse/tn3270proxy/internal/sysconfig"
)

// ErrSchemaNewer is returned by Open when the database's schema version is
// higher than this build knows how to handle (a newer binary wrote it).
var ErrSchemaNewer = errors.New("store: database schema is newer than this build")

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id              INTEGER PRIMARY KEY,
	username        TEXT UNIQUE COLLATE NOCASE NOT NULL,
	password_hash   TEXT NOT NULL,
	full_name       TEXT NOT NULL DEFAULT '',
	email           TEXT NOT NULL DEFAULT '',
	mfa_required    INTEGER NOT NULL DEFAULT 0,
	mfa_secret      TEXT NOT NULL DEFAULT '',
	mfa_enrolled_at TEXT NOT NULL DEFAULT '',
	mfa_last_step   INTEGER NOT NULL DEFAULT 0,
	user_settings_locked INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS groups (
	id   INTEGER PRIMARY KEY,
	name TEXT UNIQUE COLLATE NOCASE NOT NULL
);
CREATE TABLE IF NOT EXISTS user_groups (
	user_id  INTEGER NOT NULL REFERENCES users(id),
	group_id INTEGER NOT NULL REFERENCES groups(id),
	PRIMARY KEY (user_id, group_id)
);
CREATE TABLE IF NOT EXISTS services (
	id          INTEGER PRIMARY KEY,
	name        TEXT UNIQUE COLLATE NOCASE NOT NULL,
	description TEXT NOT NULL,
	host        TEXT NOT NULL,
	port        INTEGER NOT NULL,
	tls         INTEGER NOT NULL DEFAULT 0,
	tls_verify  INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS group_services (
	group_id   INTEGER NOT NULL REFERENCES groups(id),
	service_id INTEGER NOT NULL REFERENCES services(id),
	PRIMARY KEY (group_id, service_id)
);
CREATE TABLE IF NOT EXISTS audit (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	at          TEXT NOT NULL,
	session_id  TEXT NOT NULL,
	kind        TEXT NOT NULL,
	username    TEXT NOT NULL DEFAULT '',
	remote_addr TEXT NOT NULL DEFAULT '',
	service     TEXT NOT NULL DEFAULT '',
	detail      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS audit_at ON audit(at);
CREATE INDEX IF NOT EXISTS audit_username ON audit(username);
CREATE TABLE IF NOT EXISTS system_config (
	key   TEXT PRIMARY KEY COLLATE NOCASE NOT NULL,
	value TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS trusted_networks (
	id      INTEGER PRIMARY KEY AUTOINCREMENT,
	cidr    TEXT UNIQUE NOT NULL,
	comment TEXT NOT NULL
);
`

// migration is one numbered, forward-only schema step. Each fn runs inside a
// transaction that also stamps PRAGMA user_version to version.
type migration struct {
	version int
	name    string
	fn      func(ctx context.Context, tx *sql.Tx) error
}

// migrations is the append-only ledger. Each entry's version MUST be exactly one
// greater than the previous. Append new steps; never edit or reorder shipped ones.
var migrations = []migration{
	{1, "baseline schema", migrateV1Baseline},
}

func maxKnownVersion() int { return migrations[len(migrations)-1].version }

// migrateV1Baseline creates the pre-versioning schema. Safe on a fresh DB
// (CREATE ... IF NOT EXISTS) and on any legacy user_version=0 DB (ensureColumnTx
// fills columns the original CREATE predated).
func migrateV1Baseline(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	cols := []struct{ table, column, alter string }{
		{"services", "tls_verify", "ALTER TABLE services ADD COLUMN tls_verify INTEGER NOT NULL DEFAULT 1"},
		{"services", "description", "ALTER TABLE services ADD COLUMN description TEXT NOT NULL DEFAULT ''"},
		{"users", "full_name", "ALTER TABLE users ADD COLUMN full_name TEXT NOT NULL DEFAULT ''"},
		{"users", "email", "ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT ''"},
		{"users", "mfa_required", "ALTER TABLE users ADD COLUMN mfa_required INTEGER NOT NULL DEFAULT 0"},
		{"users", "mfa_secret", "ALTER TABLE users ADD COLUMN mfa_secret TEXT NOT NULL DEFAULT ''"},
		{"users", "mfa_enrolled_at", "ALTER TABLE users ADD COLUMN mfa_enrolled_at TEXT NOT NULL DEFAULT ''"},
		{"users", "mfa_last_step", "ALTER TABLE users ADD COLUMN mfa_last_step INTEGER NOT NULL DEFAULT 0"},
		{"users", "user_settings_locked", "ALTER TABLE users ADD COLUMN user_settings_locked INTEGER NOT NULL DEFAULT 0"},
	}
	for _, c := range cols {
		if err := ensureColumnTx(ctx, tx, c.table, c.column, c.alter); err != nil {
			return err
		}
	}
	return nil
}

// schemaVersion reads PRAGMA user_version using any query-capable handle.
func schemaVersion(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (int, error) {
	var v int
	if err := q.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("read user_version: %w", err)
	}
	return v, nil
}

// isFresh reports whether the database has no application tables yet (so there is
// nothing worth backing up before the first migration).
func isFresh(ctx context.Context, db *sql.DB) (bool, error) {
	var n int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").
		Scan(&n); err != nil {
		return false, fmt.Errorf("inspect sqlite_master: %w", err)
	}
	return n == 0, nil
}

// runMigrations brings the schema up to maxKnownVersion. Backup wiring is added
// in a later task; this version performs the stepped migration only.
func (s *Store) runMigrations(ctx context.Context) error {
	cur, err := schemaVersion(ctx, s.db)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	max := maxKnownVersion()
	if cur == max {
		return nil
	}
	slog.Default().Info("migrating database schema", "from", cur, "to", max)

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migrate: acquire conn: %w", err)
	}
	defer conn.Close()

	// Table rebuilds require foreign keys off, and PRAGMA foreign_keys is a no-op
	// inside a transaction, so toggle it on this dedicated connection around the run.
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("migrate: disable foreign keys: %w", err)
	}
	for _, m := range migrations {
		if m.version <= cur {
			continue
		}
		if err := runOne(ctx, conn, m); err != nil {
			return err
		}
		slog.Default().Debug("applied migration", "version", m.version, "name", m.name)
	}
	if err := foreignKeyCheck(ctx, conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("migrate: re-enable foreign keys: %w", err)
	}
	slog.Default().Info("database schema up to date", "version", max)
	return nil
}

// runOne applies one migration in its own transaction, stamping user_version in
// the same transaction so the version advances atomically with the change.
func runOne(ctx context.Context, conn *sql.Conn, m migration) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrate: step %d (%s): begin: %w", m.version, m.name, err)
	}
	if err := m.fn(ctx, tx); err != nil {
		tx.Rollback()
		return fmt.Errorf("migrate: step %d (%s): %w", m.version, m.name, err)
	}
	// PRAGMA values cannot be bound; m.version is a trusted int literal.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
		tx.Rollback()
		return fmt.Errorf("migrate: step %d (%s): stamp version: %w", m.version, m.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate: step %d (%s): commit: %w", m.version, m.name, err)
	}
	return nil
}

// foreignKeyCheck fails if any referential-integrity violation exists.
func foreignKeyCheck(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("migrate: foreign_key_check: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("migrate: foreign key violations after migration")
	}
	return rows.Err()
}

// reconcileDefaults seeds code-defined rows that must reach ALL databases,
// including already-migrated ones, on every Open. Deliberately OUTSIDE the
// version ledger: a new sysconfig.Catalog entry must land in existing DBs, which
// a one-time migration would never do.
func (s *Store) reconcileDefaults(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO groups (name) VALUES (?)", AdminGroup); err != nil {
		return fmt.Errorf("ensure %s group: %w", AdminGroup, err)
	}
	for _, e := range sysconfig.Catalog {
		if _, err := s.db.ExecContext(ctx,
			"INSERT OR IGNORE INTO system_config (key, value) VALUES (?, ?)",
			e.Key, e.Default); err != nil {
			return fmt.Errorf("seed system_config %s: %w", e.Key, err)
		}
	}
	return nil
}

// ensureColumnTx runs alterSQL only if table lacks column. SQLite's
// ALTER TABLE ADD COLUMN errors if the column already exists, so probe
// PRAGMA table_info first to stay idempotent.
func ensureColumnTx(ctx context.Context, tx *sql.Tx, table, column, alterSQL string) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("inspect %s: %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	if _, err := tx.ExecContext(ctx, alterSQL); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}
```

- [ ] **Step 4: Rewire `internal/store/store.go`**

In `store.go`, add the `path` field and set it in `Open`:

```go
// Store wraps the SQLite database connection.
type Store struct {
	db   *sql.DB
	path string
}
```

In `Open`, change the struct literal:

```go
	st := &Store{db: db, path: path}
```

Replace the entire old `migrate()` method (the version with the inline `s.db.Exec(schema)`, the eight `ensureColumn` calls, the ZZADMIN insert, and the sysconfig loop) with:

```go
// migrate brings the schema to the latest version, then reconciles code-defined
// default rows. See internal/store/migrate.go.
func (s *Store) migrate() error {
	ctx := context.Background()
	if err := s.runMigrations(ctx); err != nil {
		return err
	}
	return s.reconcileDefaults(ctx)
}
```

Then **delete from `store.go`**: the old `schema` const (now in migrate.go), the old `ensureColumn` method (replaced by `ensureColumnTx`), and the now-unused `sysconfig` import if nothing else in `store.go` uses it. Run `goimports`/`go build` to confirm imports.

- [ ] **Step 5: Run the new tests and the existing store suite**

Run: `go test ./internal/store/ -race -v`
Expected: PASS — the three new tests pass, and the existing legacy-ALTER + double-`Open` idempotency tests stay green.

- [ ] **Step 6: Build everything**

Run: `go build ./...`
Expected: clean build (all 8 `store.Open` callers compile unchanged).

- [ ] **Step 7: Commit**

```bash
git add internal/store/migrate.go internal/store/store.go internal/store/migrate_test.go
git commit -m "feat(store): versioned migration runner keyed on user_version (#88)"
```

---

## Task 2: Newer-DB guard

**Files:**
- Modify: `internal/store/migrate.go:runMigrations`
- Test: `internal/store/migrate_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/store/migrate_test.go`:

```go
import "errors" // add to the existing import block if not present

func TestOpenRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newer.db")

	// First open creates a current DB.
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Stamp a version one beyond what this build knows.
	if _, err := st.db.Exec("PRAGMA user_version = " +
		itoa(maxKnownVersion()+1)); err != nil {
		t.Fatalf("bump user_version: %v", err)
	}
	st.Close()

	// Reopen: must refuse.
	_, err = Open(path)
	if !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("Open newer db: err = %v, want ErrSchemaNewer", err)
	}
}

// itoa avoids importing strconv just for the test bump above.
func itoa(n int) string { return fmt.Sprintf("%d", n) }
```

Add `"fmt"` to the test file's import block if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestOpenRefusesNewerSchema -v`
Expected: FAIL — currently `Open` succeeds (no guard), so `err` is nil.

- [ ] **Step 3: Add the guard to `runMigrations`**

In `internal/store/migrate.go`, in `runMigrations`, immediately after computing `max := maxKnownVersion()` and **before** the `if cur == max` check, insert:

```go
	if cur > max {
		return fmt.Errorf("%w: db is v%d, this build supports up to v%d — upgrade the binary or restore a backup", ErrSchemaNewer, cur, max)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestOpenRefusesNewerSchema -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/migrate.go internal/store/migrate_test.go
git commit -m "feat(store): refuse to open a schema newer than the binary (#88)"
```

---

## Task 3: Backup primitive (maintenance.go)

**Files:**
- Create: `internal/store/maintenance.go`
- Test: `internal/store/maintenance_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/store/maintenance_test.go`:

```go
package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupWritesRestorableSnapshot(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "src.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()
	if _, err := st.CreateUser(ctx, "alice", "hash-a"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "snap.db")
	if err := st.Backup(ctx, dest); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// The snapshot opens and contains the row.
	snap, err := Open(dest)
	if err != nil {
		t.Fatalf("Open snapshot: %v", err)
	}
	defer snap.Close()
	if _, err := snap.GetUserByUsername(ctx, "alice"); err != nil {
		t.Fatalf("snapshot missing alice: %v", err)
	}
}

func TestBackupRefusesExistingDest(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "src2.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	dest := filepath.Join(t.TempDir(), "exists.db")
	if err := os.WriteFile(dest, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed dest: %v", err)
	}
	if err := st.Backup(ctx, dest); err == nil {
		t.Fatal("Backup overwrote an existing file; want error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestBackup' -v`
Expected: compile failure — `st.Backup` undefined.

- [ ] **Step 3: Create `internal/store/maintenance.go`**

```go
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
	"strings"
)

// execer is the subset of database/sql handles that can run a statement.
// *sql.DB, *sql.Conn, and *sql.Tx all satisfy it, so the backup path is shared
// between Store.Backup (pooled) and the migration runner (dedicated conn).
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Backup writes a consistent single-file snapshot of the database to dest using
// VACUUM INTO (safe under WAL; produces no .wal/.shm side files). dest must not
// already exist. Callable any time, including while serving. This is also the
// primitive the future admin "Backup now" action will call.
func (s *Store) Backup(ctx context.Context, dest string) error {
	return vacuumInto(ctx, s.db, dest)
}

// vacuumInto performs VACUUM INTO dest, refusing to overwrite an existing file.
// The destination is single-quote escaped because PRAGMA/VACUUM filenames are
// not reliably bindable across drivers.
func vacuumInto(ctx context.Context, e execer, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("backup: destination already exists: %s", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("backup: stat %s: %w", dest, err)
	}
	q := "VACUUM INTO '" + strings.ReplaceAll(dest, "'", "''") + "'"
	if _, err := e.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("backup: vacuum into %s: %w", dest, err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestBackup' -race -v`
Expected: PASS — both backup tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/store/maintenance.go internal/store/maintenance_test.go
git commit -m "feat(store): reusable Backup primitive via VACUUM INTO (#88)"
```

---

## Task 4: Auto-backup before migration

**Files:**
- Modify: `internal/store/migrate.go:runMigrations`
- Test: `internal/store/migrate_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/store/migrate_test.go`:

```go
import "database/sql" // ensure present in the import block

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestMigrationBacksUpLegacyDB|TestFreshDBMakesNoBackup' -v`
Expected: `TestMigrationBacksUpLegacyDB` FAILS (0 backup files — runner doesn't back up yet); `TestFreshDBMakesNoBackup` passes incidentally.

- [ ] **Step 3: Wire backup into `runMigrations`**

In `internal/store/migrate.go`, add `"time"` to the import block. In `runMigrations`, after the `if cur > max { ... }` guard and the `if cur == max { return nil }` early-return, and **before** `slog.Default().Info("migrating ...")`, insert:

```go
	fresh, err := isFresh(ctx, s.db)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if !fresh {
		dest := fmt.Sprintf("%s.pre-migrate-v%d-%d.bak", s.path, cur, time.Now().Unix())
		if err := vacuumInto(ctx, s.db, dest); err != nil {
			return fmt.Errorf("migrate: backup before migrating: %w", err)
		}
		slog.Default().Info("migration backup written", "path", dest)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestMigrationBacksUpLegacyDB|TestFreshDBMakesNoBackup' -race -v`
Expected: PASS — legacy DB produces exactly one `.bak`; fresh DB produces none.

- [ ] **Step 5: Run the full store suite**

Run: `go test ./internal/store/ -race`
Expected: PASS (the existing legacy-ALTER tests now also write a `.bak` into their temp dirs, which is harmless).

- [ ] **Step 6: Commit**

```bash
git add internal/store/migrate.go internal/store/migrate_test.go
git commit -m "feat(store): auto-backup before applying migrations (#88)"
```

---

## Task 5: Convergence & idempotency guarantees

**Files:**
- Test: `internal/store/migrate_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/store/migrate_test.go`:

```go
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
			cid     int
			name    string
			ci      colInfo
			dflt    sql.NullString
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
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestLegacyConvergesToFreshSchema|TestSecondOpenIsNoOp' -race -v`
Expected: PASS. These assert behavior already implemented in Tasks 1–4, so they should pass immediately; if `TestLegacyConvergesToFreshSchema` fails on a column mismatch, that is a real bug in `migrateV1Baseline`'s column list — fix the ALTER list to match the `schema` const, do not weaken the test.

- [ ] **Step 3: Run the full suite with the race detector**

Run: `go test ./... -race`
Expected: PASS across all packages.

- [ ] **Step 4: Commit**

```bash
git add internal/store/migrate_test.go
git commit -m "test(store): legacy→fresh schema convergence and no-op reopen (#88)"
```

---

## Task 6: Documentation

**Files:**
- Modify: `CLAUDE.md` (store package-map entry + conventions)
- Modify: `.gitignore`
- Create: `docs/operations-upgrades.md`

- [ ] **Step 1: Add `*.bak` to `.gitignore`**

Append to `.gitignore`:

```gitignore

# Pre-migration database snapshots
*.bak
```

- [ ] **Step 2: Update the `internal/store` entry in `CLAUDE.md`**

In `CLAUDE.md`, in the package map's `internal/store` paragraph, replace the sentence:

```
All Create* are idempotent (INSERT OR IGNORE).
```

with:

```
All Create* are idempotent (INSERT OR IGNORE). Schema is versioned via
PRAGMA user_version: migrate.go holds an append-only `migrations` ledger run
forward-only on Open (v1 = the pre-versioning baseline), each step in its own
transaction that stamps the version; Open refuses a DB newer than the binary
(ErrSchemaNewer) and auto-backs-up (VACUUM INTO `<db>.pre-migrate-*.bak`) before
applying anything. Code-defined seed rows (ZZADMIN group, sysconfig.Catalog
defaults) live in `reconcileDefaults`, run every Open OUTSIDE the ledger so new
catalog entries reach existing DBs. maintenance.go owns the reusable Backup
primitive (future home of Vacuum/IntegrityCheck/SchemaVersion).
```

- [ ] **Step 3: Add a convention note in `CLAUDE.md`**

In the **Conventions** section of `CLAUDE.md`, after the "Canonical uppercase names." bullet, add:

```
- **Schema changes go through the migration ledger.** A structural change or a
  data transform = append a new `{N, name, fn}` to `migrations` in
  `internal/store/migrate.go` (version exactly one above the last; never edit or
  reorder shipped steps); table rebuilds work because the runner disables foreign
  keys for the run and re-checks them. A new code-defined default (e.g. a
  `sysconfig.Catalog` entry) goes in `reconcileDefaults`, NOT a migration, so it
  reaches already-migrated DBs. Forward-only: rollback = restore the
  auto-written `*.bak` and run the old binary.
```

- [ ] **Step 4: Create `docs/operations-upgrades.md`**

```markdown
# Upgrading & database backups

tn3270proxy stores all state in a single SQLite file (`proxy.db` by default).
The schema is **versioned** and migrations are **forward-only**.

## What happens on upgrade

When a newer binary opens an older database it migrates the schema up
automatically. Before applying anything it writes a consistent snapshot next to
the database:

    proxy.db.pre-migrate-v<from>-<unixtime>.bak

Each migration runs in its own transaction and advances the recorded version
atomically, so an interrupted upgrade leaves the database at the last fully
applied version with the snapshot intact.

## Rolling back

There are no "down" migrations by design. To roll back an upgrade:

1. Stop the proxy.
2. Restore the snapshot: `mv proxy.db.pre-migrate-v<from>-<ts>.bak proxy.db`
   (move the live `proxy.db` aside first if you want to keep it).
3. Start the previous binary.

## Newer database, older binary

If you point an **older** binary at a database written by a **newer** one, it
refuses to start (`database schema is newer than this build`) rather than risk
corruption. Use the matching (or newer) binary, or restore a snapshot.

## On-demand backups

A backup can also be taken at any time via the store's `Backup` method (the
basis for the planned admin "Backup now" action). Snapshots are plain SQLite
files — verify one with `sqlite3 <file> "PRAGMA integrity_check;"`.

## Retention

Snapshots are **not** pruned automatically. They are git-ignored (`*.bak`);
remove old ones once an upgrade is confirmed healthy.
```

- [ ] **Step 5: Verify the build and full suite are still green**

Run: `go build ./... && go test ./... -race`
Expected: PASS (docs-only task; confirms nothing regressed).

- [ ] **Step 6: Commit**

```bash
git add CLAUDE.md .gitignore docs/operations-upgrades.md
git commit -m "docs: document schema migration framework and upgrade/backup ops (#88)"
```

---

## Self-Review Notes

- **Spec coverage:** runner + `user_version` (Task 1) · v1 baseline & seeding boundary (Task 1, `reconcileDefaults`) · FK handling (Task 1) · newer-DB guard (Task 2) · generic `Backup` (Task 3) · auto-backup skip-fresh/fatal/naming (Task 4) · convergence + idempotency + newer-refusal + FK-clean + backup-restorable tests (Tasks 1–5) · docs incl. `*.bak` ignore and operator note (Task 6). The anticipated `Vacuum`/`IntegrityCheck`/`SchemaVersion` admin helpers are explicitly out of scope (spec "Future work"); `maintenance.go` is structured to host them.
- **Type consistency:** `migration{version,name,fn}`, `maxKnownVersion()`, `schemaVersion()`, `isFresh()`, `runMigrations()`, `runOne()`, `foreignKeyCheck()`, `reconcileDefaults()`, `ensureColumnTx()`, `vacuumInto()`, `Store.Backup()`, `ErrSchemaNewer`, and `Store.path` are named identically across every task that references them.
- **Known, deliberate limitation:** the convergence test compares column *shape* (type/notnull/default/pk) as a name-keyed set, not exact DDL — additive `ALTER` cannot reproduce a fresh `CREATE`'s column order or `COLLATE` clause. This is documented in the test and acceptable for v1.
```