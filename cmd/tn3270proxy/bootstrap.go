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

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/quickstart"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// bootstrapCharset is the 3270-typeable unambiguous alphanumeric set re-exported
// from internal/quickstart for use by tests in this package.
const bootstrapCharset = quickstart.PasswordCharset

// generateBootstrapPassword returns a one-time password using the shared
// 3270-safe generator (see internal/quickstart.GenPassword).
func generateBootstrapPassword() (string, error) {
	return quickstart.GenPassword()
}

// adminCounter is the store subset the warning helper needs.
type adminCounter interface {
	CountAdminMembers(ctx context.Context) (int, error)
}

// warnIfNoAdmin writes a prominent warning to w when ZZADMIN has no members.
// Best-effort: a query error is logged and control returns without writing.
func warnIfNoAdmin(ctx context.Context, st adminCounter, w io.Writer) {
	n, err := st.CountAdminMembers(ctx)
	if err != nil {
		slog.Default().Error("could not check admin group", "error", err)
		return
	}
	if n == 0 {
		fmt.Fprintln(w, "WARNING: no admin user exists — the admin UI (menu entry 'A') is unreachable.")
		fmt.Fprintln(w, "         Run:  tn3270proxy bootstrap -db <db-path>")
	}
}

// runBootstrap implements the `bootstrap` subcommand. It creates the initial
// ADMIN account in ZZADMIN with a one-time crypto/rand password and prints
// that password once to w (stdout in production). It refuses when any admin
// already exists. The writer is injectable so tests can capture the printed
// password and confirm it matches the stored bcrypt hash.
func runBootstrap(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	dbPath := fs.String("db", "tn3270proxy.db", "path to SQLite database file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx := context.Background()
	n, err := st.CountAdminMembers(ctx)
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("bootstrap: an admin already exists; use the admin UI to manage accounts")
	}

	password, err := generateBootstrapPassword()
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("bootstrap: hash password: %w", err)
	}

	uid, err := st.CreateUser(ctx, "ADMIN", hash)
	if err != nil {
		return fmt.Errorf("bootstrap: create user: %w", err)
	}
	// If ADMIN existed but was not in ZZADMIN (count==0), refresh its password
	// so the printed password matches the stored hash.
	if err := st.SetPassword(ctx, uid, hash); err != nil {
		return fmt.Errorf("bootstrap: set password: %w", err)
	}
	adminGID, err := st.CreateGroup(ctx, store.AdminGroup) // idempotent; returns existing id
	if err != nil {
		return fmt.Errorf("bootstrap: get admin group: %w", err)
	}
	if err := st.AddUserToGroup(ctx, uid, adminGID); err != nil {
		return fmt.Errorf("bootstrap: add to admin group: %w", err)
	}

	fmt.Fprintf(w, "Bootstrap complete. One-time password for user ADMIN:\n\n  %s\n\n", password)
	fmt.Fprintln(w, "Log in as ADMIN, create your real admin account, then delete ADMIN.")
	return nil
}
