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

// MaxPasswordLen is the maximum password length, in bytes, that bcrypt accepts.
// Anything longer makes bcrypt.GenerateFromPassword return ErrPasswordTooLong,
// so callers should reject it up front with a clear message.
const MaxPasswordLen = 72

// ErrPasswordTooLong is returned by ValidatePassword when a password exceeds
// MaxPasswordLen bytes. It is a stable sentinel so callers (admin UI, seed) can
// distinguish "too long" from a generic store/hash failure and report it.
var ErrPasswordTooLong = errors.New("auth: password too long")

// ValidatePassword reports whether a password is acceptable for hashing. It
// checks the bcrypt byte-length limit (counting bytes, not runes, since that
// is what bcrypt enforces).
func ValidatePassword(password string) error {
	if len(password) > MaxPasswordLen {
		return ErrPasswordTooLong
	}
	return nil
}

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
