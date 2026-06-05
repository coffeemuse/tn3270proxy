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

	"github.com/CoffeeMuse/tn3270proxy/internal/sysconfig"
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
	id              INTEGER PRIMARY KEY,
	username        TEXT UNIQUE COLLATE NOCASE NOT NULL,
	password_hash   TEXT NOT NULL,
	full_name       TEXT NOT NULL DEFAULT '',
	email           TEXT NOT NULL DEFAULT '',
	mfa_required    INTEGER NOT NULL DEFAULT 0,
	mfa_secret      TEXT NOT NULL DEFAULT '',
	mfa_enrolled_at TEXT NOT NULL DEFAULT '',
	mfa_last_step   INTEGER NOT NULL DEFAULT 0
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
	// Existing DBs predating description won't get it from CREATE TABLE IF NOT
	// EXISTS, so add it explicitly (idempotent: skipped when already present).
	// Default '' is acceptable for legacy rows; new rows require non-empty via CreateService.
	if err := s.ensureColumn("services", "description",
		"ALTER TABLE services ADD COLUMN description TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("users", "full_name",
		"ALTER TABLE users ADD COLUMN full_name TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("users", "email",
		"ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("users", "mfa_required",
		"ALTER TABLE users ADD COLUMN mfa_required INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn("users", "mfa_secret",
		"ALTER TABLE users ADD COLUMN mfa_secret TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("users", "mfa_enrolled_at",
		"ALTER TABLE users ADD COLUMN mfa_enrolled_at TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("users", "mfa_last_step",
		"ALTER TABLE users ADD COLUMN mfa_last_step INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	// The reserved admin group always exists; seeding only assigns members.
	if _, err := s.db.Exec("INSERT OR IGNORE INTO groups (name) VALUES (?)", AdminGroup); err != nil {
		return fmt.Errorf("ensure %s group: %w", AdminGroup, err)
	}
	// Seed sysconfig catalog defaults (idempotent: INSERT OR IGNORE).
	for _, e := range sysconfig.Catalog {
		if _, err := s.db.Exec(
			"INSERT OR IGNORE INTO system_config (key, value) VALUES (?, ?)",
			e.Key, e.Default,
		); err != nil {
			return fmt.Errorf("seed system_config %s: %w", e.Key, err)
		}
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
	ID            int64
	Username      string
	PasswordHash  string
	FullName      string
	Email         string
	MFARequired   bool
	MFASecret     string // AES-GCM ciphertext (base64); "" = not enrolled
	MFAEnrolledAt string // UTC RFC3339; "" = not set
	MFALastStep   int64  // replay floor: highest accepted TOTP step
}

// CreateUser inserts a user, or returns the existing user's id if the
// username already exists (idempotent for seeding).
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (int64, error) {
	username = strings.ToUpper(username)
	return s.insertOrGet(ctx,
		"INSERT OR IGNORE INTO users (username, password_hash) VALUES (?, ?)",
		[]any{username, passwordHash},
		"SELECT id FROM users WHERE username = ?",
		[]any{username})
}

// CreateGroup inserts a group, or returns the existing group's id.
func (s *Store) CreateGroup(ctx context.Context, name string) (int64, error) {
	name = strings.ToUpper(name)
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
	username = strings.ToUpper(username)
	var u User
	var reqInt int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, full_name, email,
		        mfa_required, mfa_secret, mfa_enrolled_at, mfa_last_step
		 FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Email,
			&reqInt, &u.MFASecret, &u.MFAEnrolledAt, &u.MFALastStep)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.MFARequired = reqInt != 0
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
	ID          int64
	Name        string
	Description string
	Host        string
	Port        int
	TLS         bool
	TLSVerify   bool
}

const (
	MaxServiceNameLen = 8
	MaxDescriptionLen = 40
)

// NormalizeServiceName folds name to uppercase and validates it as a service
// identifier: 1-8 characters, A-Z and 0-9 only. It returns the normalized name
// or an error naming the rule violated.
func NormalizeServiceName(name string) (string, error) {
	n := strings.ToUpper(strings.TrimSpace(name))
	if n == "" {
		return "", errors.New("service name is required")
	}
	if len(n) > MaxServiceNameLen {
		return "", errors.New("service name must be 8 characters or fewer")
	}
	for _, r := range n {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return "", errors.New("service name may contain only letters A-Z and digits 0-9")
		}
	}
	return n, nil
}

// ValidateDescription enforces a required, length-bounded service label.
func ValidateDescription(desc string) error {
	if desc == "" {
		return errors.New("description is required")
	}
	// len counts bytes; descriptions are expected to be ASCII (EBCDIC display context).
	if len(desc) > MaxDescriptionLen {
		return errors.New("description must be 40 characters or fewer")
	}
	return nil
}

// ValidateFullName checks the optional display name: empty is allowed; a
// non-empty value reuses the description length cap (MaxDescriptionLen bytes).
func ValidateFullName(name string) error {
	if len(name) > MaxDescriptionLen {
		return errors.New("full name must be 40 characters or fewer")
	}
	return nil
}

// NormalizeEmail trims surrounding space and lower-cases the address, so the
// column is a clean key for future email lookups. Empty in, empty out.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail does a deliberately permissive "looks like an address" check:
// empty is allowed (the field is optional); otherwise exactly one "@", a
// non-empty local part, a domain that is non-empty and contains a ".", and no
// spaces. This is intentionally loose and is expected to loosen further later
// for legacy / pre-SMTP address forms — keep the rule in this one function.
func ValidateEmail(email string) error {
	if email == "" {
		return nil
	}
	if strings.ContainsAny(email, " \t") {
		return errors.New("email must not contain spaces")
	}
	local, domain, found := strings.Cut(email, "@")
	if !found || strings.Contains(domain, "@") {
		return errors.New("email must contain exactly one @")
	}
	if local == "" || domain == "" {
		return errors.New("email must have text before and after the @")
	}
	if !strings.Contains(domain, ".") {
		return errors.New("email domain must contain a .")
	}
	return nil
}

// CreateService inserts a service, or returns the existing service's id.
func (s *Store) CreateService(ctx context.Context, name, description, host string, port int, tls, verify bool) (int64, error) {
	name, err := NormalizeServiceName(name)
	if err != nil {
		return 0, err
	}
	if err := ValidateDescription(description); err != nil {
		return 0, err
	}
	return s.insertOrGet(ctx,
		"INSERT OR IGNORE INTO services (name, description, host, port, tls, tls_verify) VALUES (?, ?, ?, ?, ?, ?)",
		[]any{name, description, host, port, tls, verify},
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
	query := `SELECT DISTINCT s.id, s.name, s.description, s.host, s.port, s.tls, s.tls_verify
		FROM services s
		JOIN group_services gs ON gs.service_id = s.id
		JOIN groups g ON g.id = gs.group_id
		WHERE g.name IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY s.name`
	return s.queryServices(ctx, query, args...)
}
