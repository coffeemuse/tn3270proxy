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

package server

import (
	"context"
	"testing"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

func TestApplyMFAEditTogglesAndClears(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	uid, _ := st.CreateUser(ctx, "alice", "h")
	u, _ := st.GetUserByUsername(ctx, "alice")

	f := &adminFlow{store: st, identity: auth.Identity{UserID: 99, Username: "admin"}}

	// Turn MFA on.
	if msg, err := f.applyMFAEdit(ctx, u, map[string]string{
		screens.FieldMFARequired: "Y", screens.FieldMFAClear: "",
	}); err != nil || msg != "" {
		t.Fatalf("enable MFA: msg=%q err=%v", msg, err)
	}
	u, _ = st.GetUserByUsername(ctx, "alice")
	if !u.MFARequired {
		t.Fatal("MFA required should be set")
	}

	// Simulate an enrolled secret, then clear it.
	st.StoreMFAEnrollment(ctx, uid, "ct", "t", 3)
	u, _ = st.GetUserByUsername(ctx, "alice")
	if msg, err := f.applyMFAEdit(ctx, u, map[string]string{
		screens.FieldMFARequired: "Y", screens.FieldMFAClear: "Y",
	}); err != nil || msg != "" {
		t.Fatalf("clear MFA: msg=%q err=%v", msg, err)
	}
	u, _ = st.GetUserByUsername(ctx, "alice")
	if u.MFASecret != "" {
		t.Fatal("clear should wipe the secret")
	}
	if !u.MFARequired {
		t.Fatal("clear must preserve the required flag")
	}
}

func TestApplyMFAEditRejectsBadToggle(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/s.db")
	defer st.Close()
	ctx := context.Background()
	st.CreateUser(ctx, "alice", "h")
	u, _ := st.GetUserByUsername(ctx, "alice")
	f := &adminFlow{store: st, identity: auth.Identity{UserID: 99, Username: "admin"}}
	msg, err := f.applyMFAEdit(ctx, u, map[string]string{screens.FieldMFARequired: "X"})
	if err != nil || msg == "" {
		t.Fatalf("bad toggle should return a non-empty errMsg, got msg=%q err=%v", msg, err)
	}
}
