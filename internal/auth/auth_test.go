package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func seedUser(t *testing.T) (*store.Store, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	hash, err := HashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := st.CreateUser(ctx, "alice", hash)
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := st.CreateGroup(ctx, "ops")
	st.AddUserToGroup(ctx, uid, gid)
	return st, "alice"
}

func TestAuthenticateSuccess(t *testing.T) {
	st, user := seedUser(t)
	id, err := Authenticate(context.Background(), st, user, "s3cret")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if id.Username != "alice" || len(id.Groups) != 1 || id.Groups[0] != "ops" {
		t.Errorf("identity = %+v", id)
	}
}

func TestAuthenticateWrongPassword(t *testing.T) {
	st, user := seedUser(t)
	_, err := Authenticate(context.Background(), st, user, "wrong")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestAuthenticateUnknownUserSameError(t *testing.T) {
	st, _ := seedUser(t)
	_, err := Authenticate(context.Background(), st, "nobody", "whatever")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("err = %v, want ErrInvalidCredentials (no user-existence leak)", err)
	}
}

func TestHashPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" || hash == "s3cret" {
		t.Fatalf("hash = %q", hash)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("s3cret")); err != nil {
		t.Errorf("hash does not verify: %v", err)
	}
}
