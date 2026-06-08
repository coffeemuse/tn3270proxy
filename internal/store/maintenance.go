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
	"os"
)

// execer is the subset of database/sql handles that can run a statement.
// *sql.DB, *sql.Conn, and *sql.Tx all satisfy it, so the backup path is shared
// between Store.Backup (pooled) and the migration runner (dedicated conn).
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Backup writes a consistent single-file snapshot of the database to dest using
// VACUUM INTO (safe under WAL; produces no .wal/.shm side files). dest must not
// already exist. Callable any time, including while serving. This is also the
// primitive the future admin "Backup now" action will call.
func (s *Store) Backup(ctx context.Context, dest string) error {
	return vacuumInto(ctx, s.db, dest)
}

// vacuumInto performs VACUUM INTO dest, refusing to overwrite an existing file.
// The destination is passed as a bound parameter (supported by the modernc
// driver / SQLite >= 3.27), avoiding any string-built SQL.
func vacuumInto(ctx context.Context, e execer, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("backup: destination already exists: %s", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("backup: stat %s: %w", dest, err)
	}
	if _, err := e.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return fmt.Errorf("backup: vacuum into %s: %w", dest, err)
	}
	return nil
}
