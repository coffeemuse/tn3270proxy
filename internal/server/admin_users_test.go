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

	"github.com/coffeemuse/tn3270proxy/internal/auth"
	"github.com/coffeemuse/tn3270proxy/internal/screens"
	"github.com/coffeemuse/tn3270proxy/internal/store"
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
		screens.FieldMFARequired: "Y", screens.FieldMFAClear: "Y", screens.FieldMFAClearConfirm: "CLEAR",
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

func TestApplyLockEditTogglesAndAudits(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	st.CreateUser(ctx, "guest", "h")
	u, _ := st.GetUserByUsername(ctx, "guest")

	var kinds []string
	f := &adminFlow{
		store:    st,
		identity: auth.Identity{UserID: 99, Username: "admin"},
		audit:    func(_ context.Context, e store.AuditEvent) { kinds = append(kinds, e.Kind) },
	}

	// Lock.
	if msg, err := f.applyLockEdit(ctx, u, map[string]string{
		screens.FieldUserSettingsLocked: "Y",
	}); err != nil || msg != "" {
		t.Fatalf("lock: msg=%q err=%v", msg, err)
	}
	u, _ = st.GetUserByUsername(ctx, "guest")
	if !u.UserSettingsLocked {
		t.Fatal("expected locked")
	}

	// Unlock.
	if msg, err := f.applyLockEdit(ctx, u, map[string]string{
		screens.FieldUserSettingsLocked: "N",
	}); err != nil || msg != "" {
		t.Fatalf("unlock: msg=%q err=%v", msg, err)
	}
	u, _ = st.GetUserByUsername(ctx, "guest")
	if u.UserSettingsLocked {
		t.Fatal("expected unlocked")
	}

	if len(kinds) != 2 || kinds[0] != store.AuditSettingsLocked || kinds[1] != store.AuditSettingsUnlocked {
		t.Fatalf("audit kinds = %v", kinds)
	}

	// No-op: submitting the current value (already unlocked) writes nothing and
	// emits no audit event.
	if msg, err := f.applyLockEdit(ctx, u, map[string]string{
		screens.FieldUserSettingsLocked: "N",
	}); err != nil || msg != "" {
		t.Fatalf("no-op: msg=%q err=%v", msg, err)
	}
	if len(kinds) != 2 {
		t.Fatalf("no-op must not audit; kinds = %v", kinds)
	}
}

func TestApplyLockEditRejectsBadValue(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/s.db")
	defer st.Close()
	ctx := context.Background()
	st.CreateUser(ctx, "guest", "h")
	u, _ := st.GetUserByUsername(ctx, "guest")
	f := &adminFlow{store: st, identity: auth.Identity{UserID: 99, Username: "admin"}}
	if msg, err := f.applyLockEdit(ctx, u, map[string]string{
		screens.FieldUserSettingsLocked: "x",
	}); err != nil || msg == "" {
		t.Fatalf("want rejection message, got msg=%q err=%v", msg, err)
	}
}

func TestUserSaveEditRejectsClearWithoutConfirmBeforeCommit(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	origHash, _ := auth.HashPassword("orig")
	uid, _ := st.CreateUser(ctx, "alice", origHash)
	st.StoreMFAEnrollment(ctx, uid, "ct", "t", 3)
	u, _ := st.GetUserByUsername(ctx, "alice")

	f := &adminFlow{store: st, identity: auth.Identity{UserID: 99, Username: "admin"}}

	// New password supplied AND Clear-MFA=Y but no typed CLEAR -> must reject and
	// NOT change the password (no partial commit).
	msg, err := f.userSaveEdit(ctx, u, map[string]string{
		screens.FieldPassword:        "newpass",
		screens.FieldRetype:          "newpass",
		screens.FieldMFAClear:        "Y",
		screens.FieldMFAClearConfirm: "",
	}, "Alice Example", "alice@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "TYPE CLEAR TO CONFIRM MFA WIPE" {
		t.Errorf("msg = %q, want the confirm prompt", msg)
	}
	got, _ := st.GetUserByUsername(ctx, "alice")
	if got.PasswordHash != origHash {
		t.Errorf("password was changed despite the rejected submit")
	}
}

func TestApplyMFAEditClearRequiresTypedConfirm(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	uid, _ := st.CreateUser(ctx, "alice", "h")
	st.StoreMFAEnrollment(ctx, uid, "ct", "t", 3) // give alice an enrolled secret
	u, _ := st.GetUserByUsername(ctx, "alice")

	f := &adminFlow{store: st, identity: auth.Identity{UserID: 99, Username: "admin"}}

	// Toggle Y but confirm field blank -> rejected, secret retained.
	msg, err := f.applyMFAEdit(ctx, u, map[string]string{
		screens.FieldMFAClear: "Y",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "TYPE CLEAR TO CONFIRM MFA WIPE" {
		t.Errorf("msg = %q, want the confirm prompt", msg)
	}
	if got, _ := st.GetUserByUsername(ctx, "alice"); got.MFASecret == "" {
		t.Errorf("secret was wiped without confirmation")
	}

	// Toggle Y + typed CLEAR (case-insensitive) -> wiped.
	msg, err = f.applyMFAEdit(ctx, u, map[string]string{
		screens.FieldMFAClear:        "Y",
		screens.FieldMFAClearConfirm: "clear",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "" {
		t.Errorf("msg = %q, want empty (wipe succeeded)", msg)
	}
	if got, _ := st.GetUserByUsername(ctx, "alice"); got.MFASecret != "" {
		t.Errorf("secret not wiped after confirmed clear")
	}
}
