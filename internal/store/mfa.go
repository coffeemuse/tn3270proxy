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
)

// mfaSentinelKey is the reserved system_config key holding a known plaintext
// encrypted under the master key. It is NOT a sysconfig.Catalog entry, so it
// never appears in the admin System Parameters form.
const mfaSentinelKey = "MFA_KEY_CHECK"

// SetMFARequired sets the admin enforce flag. Returns ErrNotFound for an
// unknown user id.
func (s *Store) SetMFARequired(ctx context.Context, userID int64, required bool) error {
	return s.execExpectingRow(ctx,
		"UPDATE users SET mfa_required = ? WHERE id = ?", boolToInt(required), userID)
}

// StoreMFAEnrollment arms MFA: it stores the encrypted secret, the enrollment
// timestamp, and the initial replay floor. Returns ErrNotFound for an unknown
// user id.
func (s *Store) StoreMFAEnrollment(ctx context.Context, userID int64, encSecret, enrolledAt string, step int64) error {
	return s.execExpectingRow(ctx,
		"UPDATE users SET mfa_secret = ?, mfa_enrolled_at = ?, mfa_last_step = ? WHERE id = ?",
		encSecret, enrolledAt, step, userID)
}

// UpdateMFAStep advances the replay floor after a successful verification.
func (s *Store) UpdateMFAStep(ctx context.Context, userID int64, step int64) error {
	return s.execExpectingRow(ctx,
		"UPDATE users SET mfa_last_step = ? WHERE id = ?", step, userID)
}

// ClearMFA wipes the secret material (lost-device recovery), preserving the
// mfa_required flag so the user re-enrolls on next login if still required.
func (s *Store) ClearMFA(ctx context.Context, userID int64) error {
	return s.execExpectingRow(ctx,
		"UPDATE users SET mfa_secret = '', mfa_enrolled_at = '', mfa_last_step = 0 WHERE id = ?",
		userID)
}

// CountEnrolledUsers returns how many users have a stored secret (used by the
// startup fail-closed check).
func (s *Store) CountEnrolledUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM users WHERE mfa_secret != ''").Scan(&n)
	return n, err
}

// ResetAllMFA wipes every stored secret (break-glass key recovery). It returns
// the number of users reset. mfa_required is preserved.
func (s *Store) ResetAllMFA(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx,
		"UPDATE users SET mfa_secret = '', mfa_enrolled_at = '', mfa_last_step = 0 WHERE mfa_secret != ''")
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// GetMFASentinel returns the stored key-check sentinel, or "" if none exists.
func (s *Store) GetMFASentinel(ctx context.Context) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx,
		"SELECT value FROM system_config WHERE key = ?", mfaSentinelKey).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

// SetMFASentinel writes (inserts or replaces) the key-check sentinel.
func (s *Store) SetMFASentinel(ctx context.Context, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO system_config (key, value) VALUES (?, ?)",
		mfaSentinelKey, value)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
