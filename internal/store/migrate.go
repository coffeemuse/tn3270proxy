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
	"time"

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
	description TEXT NOT NULL DEFAULT '',
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
	{2, "audit actor column", migrateV2AuditActor},
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

// migrateV2AuditActor adds the audit.actor column (actor vs subject split, GH #73):
// actor = who performed the action, distinct from username = the subject. Existing
// rows are backfilled actor=username (best available identity for historical rows;
// the true admin behind a pre-v2 admin-on-other row is unrecoverable). Indexed for
// the actor-centric query lens. Runs once during the v1->v2 upgrade.
func migrateV2AuditActor(ctx context.Context, tx *sql.Tx) error {
	if err := ensureColumnTx(ctx, tx, "audit", "actor",
		"ALTER TABLE audit ADD COLUMN actor TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE audit SET actor = username"); err != nil {
		return fmt.Errorf("backfill audit.actor: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"CREATE INDEX IF NOT EXISTS audit_actor ON audit(actor)"); err != nil {
		return fmt.Errorf("create audit_actor index: %w", err)
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

// runMigrations brings the schema up to maxKnownVersion. It refuses a database
// newer than this build, writes a pre-migration backup (unless the database is
// fresh), then applies each pending step on a dedicated connection with foreign
// keys disabled, re-checking referential integrity at the end.
func (s *Store) runMigrations(ctx context.Context) error {
	cur, err := schemaVersion(ctx, s.db)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	max := maxKnownVersion()
	if cur > max {
		return fmt.Errorf("%w: db is v%d, this build supports up to v%d; upgrade the binary or restore a backup", ErrSchemaNewer, cur, max)
	}
	if cur == max {
		return nil
	}
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
	// Restore FK enforcement on the pooled connection regardless of outcome.
	// (The modernc driver does not re-apply the DSN foreign_keys pragma on conn
	// reuse, so a conn returned FK-OFF would serve later queries FK-OFF.)
	defer func() {
		if _, err := conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); err != nil {
			slog.Default().Error("migrate: failed to restore foreign_keys after migration", "err", err)
		}
	}()
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
	defer tx.Rollback() // no-op after a successful Commit; cleans up on any error path

	if err := m.fn(ctx, tx); err != nil {
		return fmt.Errorf("migrate: step %d (%s): %w", m.version, m.name, err)
	}
	// PRAGMA values cannot be bound; m.version is a trusted int literal.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
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
// PRAGMA table_info first to stay idempotent. table must be a trusted
// internal constant (PRAGMA table_info does not support bound parameters).
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
