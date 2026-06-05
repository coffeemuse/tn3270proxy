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

// GetConfig returns the current value for key from system_config.
// Returns ErrNotFound if the key is not present (i.e. not in the catalog).
func (s *Store) GetConfig(ctx context.Context, key string) (string, error) {
	var val string
	err := s.db.QueryRowContext(ctx,
		"SELECT value FROM system_config WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

// SetConfig updates the value for an existing catalog key in system_config.
// Returns ErrNotFound if the key does not exist (i.e. was never seeded).
func (s *Store) SetConfig(ctx context.Context, key, value string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE system_config SET value = ? WHERE key = ?", value, key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
