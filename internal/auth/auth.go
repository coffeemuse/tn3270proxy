package auth

import (
	"context"
	"errors"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// ErrInvalidCredentials is returned for any authentication failure. It is
// intentionally identical for unknown users and wrong passwords so callers
// cannot distinguish the two (no username-enumeration leak).
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// Identity is the result of a successful authentication.
type Identity struct {
	UserID   int64
	Username string
	Groups   []string
}

// UserStore is the subset of *store.Store that auth needs.
type UserStore interface {
	GetUserByUsername(ctx context.Context, username string) (store.User, error)
	GetUserGroups(ctx context.Context, userID int64) ([]string, error)
}

// HashPassword bcrypt-hashes a plaintext password for storage. It is the
// single hashing path: seed and the admin UI both use it, so cost/algorithm
// changes happen in one place.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// Authenticate verifies the username/password against the store and returns
// the user's Identity (including group memberships) on success.
func Authenticate(ctx context.Context, st UserStore, username, password string) (Identity, error) {
	u, err := st.GetUserByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		// Compare against a dummy hash to keep timing roughly constant.
		bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinvalidinvalidinvalidinvalidinva"), []byte(password))
		return Identity{}, ErrInvalidCredentials
	}
	if err != nil {
		return Identity{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return Identity{}, ErrInvalidCredentials
	}
	groups, err := st.GetUserGroups(ctx, u.ID)
	if err != nil {
		return Identity{}, err
	}
	return Identity{UserID: u.ID, Username: u.Username, Groups: groups}, nil
}
