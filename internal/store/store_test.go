package store

import (
	"context"
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
