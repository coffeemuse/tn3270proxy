package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
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
	id   INTEGER PRIMARY KEY,
	name TEXT UNIQUE NOT NULL,
	host TEXT NOT NULL,
	port INTEGER NOT NULL,
	tls  INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS group_services (
	group_id   INTEGER NOT NULL REFERENCES groups(id),
	service_id INTEGER NOT NULL REFERENCES services(id),
	PRIMARY KEY (group_id, service_id)
);
`

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
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
