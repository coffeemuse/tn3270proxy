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
	"strings"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("store: not found")

// Store wraps the SQLite database connection.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite database at path and applies
// the schema migration. The schema is idempotent.
func Open(path string) (*Store, error) {
	// DSN _pragma applies to every pooled connection; db.Exec("PRAGMA ...") would only configure one.
	const pragmas = "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", path+pragmas)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(4) // WAL allows concurrent readers + one writer; 4 bounds pool without serializing.
	st := &Store{db: db}
	if err := st.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return st, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY,
	username      TEXT UNIQUE NOT NULL,
	password_hash TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS groups (
	id   INTEGER PRIMARY KEY,
	name TEXT UNIQUE NOT NULL
);
CREATE TABLE IF NOT EXISTS user_groups (
	user_id  INTEGER NOT NULL REFERENCES users(id),
	group_id INTEGER NOT NULL REFERENCES groups(id),
	PRIMARY KEY (user_id, group_id)
);
CREATE TABLE IF NOT EXISTS services (
	id         INTEGER PRIMARY KEY,
	name       TEXT UNIQUE NOT NULL,
	host       TEXT NOT NULL,
	port       INTEGER NOT NULL,
	tls        INTEGER NOT NULL DEFAULT 0,
	tls_verify INTEGER NOT NULL DEFAULT 1
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
`

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	// Existing DBs predating tls_verify won't get it from CREATE TABLE IF NOT
	// EXISTS, so add it explicitly (idempotent: skipped when already present).
	if err := s.ensureColumn("services", "tls_verify",
		"ALTER TABLE services ADD COLUMN tls_verify INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	// The reserved admin group always exists; seeding only assigns members.
	if _, err := s.db.Exec("INSERT OR IGNORE INTO groups (name) VALUES (?)", AdminGroup); err != nil {
		return fmt.Errorf("ensure %s group: %w", AdminGroup, err)
	}
	return nil
}

// ensureColumn runs alterSQL only if table lacks column. SQLite's
// ALTER TABLE ADD COLUMN errors if the column already exists, so we probe
// PRAGMA table_info first to keep migrate() idempotent.
func (s *Store) ensureColumn(table, column, alterSQL string) error {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
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
			return nil // already present; defer rows.Close() handles cleanup
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	if _, err := s.db.Exec(alterSQL); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

// User is an account record.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
}

// CreateUser inserts a user, or returns the existing user's id if the
// username already exists (idempotent for seeding).
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (int64, error) {
	return s.insertOrGet(ctx,
		"INSERT OR IGNORE INTO users (username, password_hash) VALUES (?, ?)",
		[]any{username, passwordHash},
		"SELECT id FROM users WHERE username = ?",
		[]any{username})
}

// CreateGroup inserts a group, or returns the existing group's id.
func (s *Store) CreateGroup(ctx context.Context, name string) (int64, error) {
	return s.insertOrGet(ctx,
		"INSERT OR IGNORE INTO groups (name) VALUES (?)",
		[]any{name},
		"SELECT id FROM groups WHERE name = ?",
		[]any{name})
}

// AddUserToGroup links a user to a group (idempotent).
func (s *Store) AddUserToGroup(ctx context.Context, userID, groupID int64) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO user_groups (user_id, group_id) VALUES (?, ?)",
		userID, groupID)
	return err
}

// GetUserByUsername returns the user, or ErrNotFound.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		"SELECT id, username, password_hash FROM users WHERE username = ?", username).
		Scan(&u.ID, &u.Username, &u.PasswordHash)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	return u, nil
}

// GetUserGroups returns the names of groups the user belongs to.
func (s *Store) GetUserGroups(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT g.name FROM groups g
		 JOIN user_groups ug ON ug.group_id = g.id
		 WHERE ug.user_id = ? ORDER BY g.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// insertOrGet runs an INSERT OR IGNORE; if a row was inserted it returns the
// new id, otherwise it runs selectSQL to fetch the existing id.
func (s *Store) insertOrGet(ctx context.Context, insertSQL string, insertArgs []any, selectSQL string, selectArgs []any) (int64, error) {
	res, err := s.db.ExecContext(ctx, insertSQL, insertArgs...)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return res.LastInsertId()
	}
	var id int64
	if err := s.db.QueryRowContext(ctx, selectSQL, selectArgs...).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// Service is a backend TN3270 host the menu can offer.
type Service struct {
	ID        int64
	Name      string
	Host      string
	Port      int
	TLS       bool
	TLSVerify bool
}

// CreateService inserts a service, or returns the existing service's id.
func (s *Store) CreateService(ctx context.Context, name, host string, port int, tls, verify bool) (int64, error) {
	tlsInt := 0
	if tls {
		tlsInt = 1
	}
	verifyInt := 0
	if verify {
		verifyInt = 1
	}
	return s.insertOrGet(ctx,
		"INSERT OR IGNORE INTO services (name, host, port, tls, tls_verify) VALUES (?, ?, ?, ?, ?)",
		[]any{name, host, port, tlsInt, verifyInt},
		"SELECT id FROM services WHERE name = ?",
		[]any{name})
}

// LinkGroupService grants a group access to a service (idempotent).
func (s *Store) LinkGroupService(ctx context.Context, groupID, serviceID int64) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO group_services (group_id, service_id) VALUES (?, ?)",
		groupID, serviceID)
	return err
}

// ListServicesForGroups returns the distinct services visible to any of the
// named groups, ordered by service name.
func (s *Store) ListServicesForGroups(ctx context.Context, groups []string) ([]Service, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(groups))
	args := make([]any, len(groups))
	for i, g := range groups {
		placeholders[i] = "?"
		args[i] = g
	}
	query := `SELECT DISTINCT s.id, s.name, s.host, s.port, s.tls, s.tls_verify
		FROM services s
		JOIN group_services gs ON gs.service_id = s.id
		JOIN groups g ON g.id = gs.group_id
		WHERE g.name IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY s.name`
	return s.queryServices(ctx, query, args...)
}
