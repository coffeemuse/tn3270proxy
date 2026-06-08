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
	"errors"
	"testing"
)

func TestUserSettingsLockedDefaultsOff(t *testing.T) {
	st, err := Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	st.CreateUser(ctx, "alice", "h")
	u, err := st.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if u.UserSettingsLocked {
		t.Fatal("new user must default to unlocked")
	}
}

func TestSetUserSettingsLockedRoundTrips(t *testing.T) {
	st, _ := Open(t.TempDir() + "/s.db")
	defer st.Close()
	ctx := context.Background()
	uid, _ := st.CreateUser(ctx, "alice", "h")

	if err := st.SetUserSettingsLocked(ctx, uid, true); err != nil {
		t.Fatalf("set true: %v", err)
	}
	u, _ := st.GetUserByUsername(ctx, "alice")
	if !u.UserSettingsLocked {
		t.Fatal("expected locked after set true")
	}
	// Verify the locked state through the list path too (guards against a
	// column-order bug in queryUsers that the unlocked check below would miss).
	locked, _ := st.ListUsers(ctx)
	if len(locked) != 1 || !locked[0].UserSettingsLocked {
		t.Fatalf("list path (locked): %+v", locked)
	}

	if err := st.SetUserSettingsLocked(ctx, uid, false); err != nil {
		t.Fatalf("set false: %v", err)
	}
	u, _ = st.GetUserByUsername(ctx, "alice")
	if u.UserSettingsLocked {
		t.Fatal("expected unlocked after set false")
	}

	// Also visible through the list path.
	users, _ := st.ListUsers(ctx)
	if len(users) != 1 || users[0].UserSettingsLocked {
		t.Fatalf("list path: %+v", users)
	}
}

func TestSetUserSettingsLockedUnknownUser(t *testing.T) {
	st, _ := Open(t.TempDir() + "/s.db")
	defer st.Close()
	if err := st.SetUserSettingsLocked(context.Background(), 999, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
