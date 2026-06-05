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
	"testing"
)

func newMFAStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	st, err := Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, context.Background()
}

func TestMFAEnrollmentLifecycle(t *testing.T) {
	st, ctx := newMFAStore(t)
	id, _ := st.CreateUser(ctx, "alice", "hash")

	if err := st.SetMFARequired(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.CountEnrolledUsers(ctx); n != 0 {
		t.Fatalf("required but not enrolled should count 0, got %d", n)
	}
	if err := st.StoreMFAEnrollment(ctx, id, "ciphertext", "2026-06-05T00:00:00Z", 7); err != nil {
		t.Fatal(err)
	}
	u, _ := st.GetUserByUsername(ctx, "alice")
	if !u.MFARequired || u.MFASecret != "ciphertext" || u.MFAEnrolledAt == "" || u.MFALastStep != 7 {
		t.Fatalf("after enroll: %+v", u)
	}
	if n, _ := st.CountEnrolledUsers(ctx); n != 1 {
		t.Fatalf("want 1 enrolled, got %d", n)
	}
	if err := st.UpdateMFAStep(ctx, id, 9); err != nil {
		t.Fatal(err)
	}
	u, _ = st.GetUserByUsername(ctx, "alice")
	if u.MFALastStep != 9 {
		t.Fatalf("want step 9, got %d", u.MFALastStep)
	}
	if err := st.ClearMFA(ctx, id); err != nil {
		t.Fatal(err)
	}
	u, _ = st.GetUserByUsername(ctx, "alice")
	if u.MFASecret != "" || u.MFAEnrolledAt != "" || u.MFALastStep != 0 {
		t.Fatalf("after clear, secret fields should reset (required preserved): %+v", u)
	}
	if !u.MFARequired {
		t.Fatal("clear must preserve mfa_required")
	}
}

func TestResetAllMFA(t *testing.T) {
	st, ctx := newMFAStore(t)
	a, _ := st.CreateUser(ctx, "alice", "h")
	b, _ := st.CreateUser(ctx, "bob", "h")
	st.SetMFARequired(ctx, a, true)
	st.SetMFARequired(ctx, b, true)
	st.StoreMFAEnrollment(ctx, a, "ct", "t", 1)
	st.StoreMFAEnrollment(ctx, b, "ct", "t", 1)
	n, err := st.ResetAllMFA(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2 reset, got %d", n)
	}
	if c, _ := st.CountEnrolledUsers(ctx); c != 0 {
		t.Fatalf("want 0 enrolled after reset, got %d", c)
	}
}

func TestMFASentinel(t *testing.T) {
	st, ctx := newMFAStore(t)
	got, err := st.GetMFASentinel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("absent sentinel should be empty, got %q", got)
	}
	if err := st.SetMFASentinel(ctx, "sealed-value"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetMFASentinel(ctx)
	if got != "sealed-value" {
		t.Fatalf("want round-trip, got %q", got)
	}
	if err := st.SetMFASentinel(ctx, "v2"); err != nil { // upsert
		t.Fatal(err)
	}
	got, _ = st.GetMFASentinel(ctx)
	if got != "v2" {
		t.Fatalf("want overwrite, got %q", got)
	}
}
