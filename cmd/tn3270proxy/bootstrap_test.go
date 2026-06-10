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
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coffeemuse/tn3270proxy/internal/quickstart"
	"github.com/coffeemuse/tn3270proxy/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// fakeAdminCounter implements adminCounter for warning-helper tests.
type fakeAdminCounter struct {
	n   int
	err error
}

func (f fakeAdminCounter) CountAdminMembers(_ context.Context) (int, error) {
	return f.n, f.err
}

func TestWarnIfNoAdminWritesWarningWhenEmpty(t *testing.T) {
	var buf bytes.Buffer
	warnIfNoAdmin(context.Background(), fakeAdminCounter{n: 0}, &buf)
	out := buf.String()
	if !strings.Contains(out, "WARNING") {
		t.Errorf("expected WARNING in output; got %q", out)
	}
	if !strings.Contains(out, "bootstrap") {
		t.Errorf("expected 'bootstrap' command hint in output; got %q", out)
	}
}

func TestWarnIfNoAdminSilentWhenAdminExists(t *testing.T) {
	var buf bytes.Buffer
	warnIfNoAdmin(context.Background(), fakeAdminCounter{n: 1}, &buf)
	if buf.Len() != 0 {
		t.Errorf("unexpected output when admin exists: %q", buf.String())
	}
}

func TestWarnIfNoAdminSilentOnError(t *testing.T) {
	var buf bytes.Buffer
	warnIfNoAdmin(context.Background(), fakeAdminCounter{err: errors.New("db error")}, &buf)
	if buf.Len() != 0 {
		t.Errorf("unexpected write to w on query error: %q", buf.String())
	}
}

func TestGenerateBootstrapPasswordFormat(t *testing.T) {
	for i := 0; i < 50; i++ {
		pw, err := generateBootstrapPassword()
		if err != nil {
			t.Fatalf("generateBootstrapPassword: %v", err)
		}
		// Expected format: XXXX-XXXX-XXXX (14 chars total)
		if len(pw) != 14 {
			t.Errorf("password len = %d, want 14 (XXXX-XXXX-XXXX): %q", len(pw), pw)
		}
		parts := strings.Split(pw, "-")
		if len(parts) != 3 {
			t.Errorf("password part count = %d, want 3: %q", len(parts), pw)
		}
		for _, part := range parts {
			if len(part) != 4 {
				t.Errorf("part len = %d, want 4: %q", len(part), pw)
			}
		}
	}
}

func TestGenerateBootstrapPasswordCharset(t *testing.T) {
	for i := 0; i < 200; i++ {
		pw, err := generateBootstrapPassword()
		if err != nil {
			t.Fatalf("generateBootstrapPassword: %v", err)
		}
		for _, c := range strings.ReplaceAll(pw, "-", "") {
			if !strings.ContainsRune(quickstart.PasswordCharset, c) {
				t.Errorf("char %c not in quickstart.PasswordCharset: %q", c, pw)
			}
		}
		// Verify excluded ambiguous chars never appear.
		for _, excluded := range "01IOlo" {
			if strings.ContainsRune(pw, excluded) {
				t.Errorf("ambiguous char %c found in password: %q", excluded, pw)
			}
		}
	}
}

func TestRunBootstrapCreatesFreshDB(t *testing.T) {
	db := filepath.Join(t.TempDir(), "b.db")
	var buf bytes.Buffer
	if err := runBootstrap([]string{"-db", db}, &buf); err != nil {
		t.Fatalf("runBootstrap: %v", err)
	}

	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	// ZZADMIN must have exactly one member.
	n, err := st.CountAdminMembers(ctx)
	if err != nil {
		t.Fatalf("CountAdminMembers: %v", err)
	}
	if n != 1 {
		t.Errorf("admin member count = %d, want 1", n)
	}

	// User ADMIN must exist with a bcrypt hash.
	u, err := st.GetUserByUsername(ctx, "ADMIN")
	if err != nil {
		t.Fatalf("GetUserByUsername(ADMIN): %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("wrongpassword")); err == nil {
		t.Error("bcrypt accepted wrong password — hash is not a real bcrypt hash")
	}
	// Hash must start with a valid bcrypt prefix.
	if !strings.HasPrefix(u.PasswordHash, "$2") {
		t.Errorf("password hash %q does not look like bcrypt", u.PasswordHash)
	}

	// The operator's whole flow depends on the *printed* password matching the
	// stored hash. Extract it from the output and verify against the bcrypt hash.
	printed := extractBootstrapPassword(t, buf.String())
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(printed)); err != nil {
		t.Errorf("printed password %q does not match stored hash: %v", printed, err)
	}
}

// extractBootstrapPassword pulls the one-time password (the XXXX-XXXX-XXXX
// token) out of runBootstrap's output.
func extractBootstrapPassword(t *testing.T, out string) string {
	t.Helper()
	for _, f := range strings.Fields(out) {
		parts := strings.Split(f, "-")
		if len(parts) != 3 {
			continue
		}
		ok := true
		for _, p := range parts {
			if len(p) != 4 {
				ok = false
				break
			}
		}
		if ok {
			return f
		}
	}
	t.Fatalf("no XXXX-XXXX-XXXX password found in output: %q", out)
	return ""
}

func TestRunBootstrapRefusesWhenAdminExists(t *testing.T) {
	db := filepath.Join(t.TempDir(), "b.db")
	if err := runBootstrap([]string{"-db", db}, io.Discard); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	err := runBootstrap([]string{"-db", db}, io.Discard)
	if err == nil {
		t.Fatal("second bootstrap: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "admin already exists") {
		t.Errorf("error = %q; want mention of 'admin already exists'", err.Error())
	}
}
