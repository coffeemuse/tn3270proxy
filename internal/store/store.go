package store

import (
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
