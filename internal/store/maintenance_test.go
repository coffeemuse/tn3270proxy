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
