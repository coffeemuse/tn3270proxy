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
	"net"
	"slices"
	"testing"
	"time"

	"github.com/coffeemuse/tn3270proxy/internal/auth"
	"github.com/coffeemuse/tn3270proxy/internal/screens"
	"github.com/coffeemuse/tn3270proxy/internal/store"
	"github.com/coffeemuse/tn3270proxy/internal/ui3270"
)

func TestSelfChangePassword(t *testing.T) {
	ctx := context.Background()

	// Build a real store and seed ALICE with a known bcrypt hash.
	p := &fakePresenter{termType: "IBM-3278-2-E"}
	s := newTestSession(t, p, &fakeBridger{})
	s.Authenticate = auth.Authenticate
	s.Throttle = newAuthThrottle()
	s.Now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	var slept []time.Duration
	s.Sleep = func(d time.Duration) { slept = append(slept, d) }

	// Seed ALICE with "oldpass" (bcrypt hashed). newTestSession pre-creates alice
	// with a different hash; SetPassword overwrites it so auth.Authenticate works.
	hash, err := auth.HashPassword("oldpass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	uid, err := s.Store.CreateUser(ctx, "ALICE", hash)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.Store.SetPassword(ctx, uid, hash); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	// Wire a fake renderer that returns the change-password form values.
	fp := &fakeAdminPresenter{
		forms: []ui3270.FormAction{{Values: map[string]string{
			screens.FieldCurrentPassword: "oldpass",
			screens.FieldPassword:        "newpass",
			screens.FieldRetype:          "newpass",
		}}},
	}
	s.AdminRenderer = func(_ net.Conn, _ Term) ui3270.Renderer { return fp }

	// Build an audit trail backed by a recording auditor.
	rec := &recordingAuditor{}
	aud := &auditTrail{auditor: rec}

	term := Term{Type: "IBM-3278-2", Rows: 24, Cols: 80}
	identity := auth.Identity{UserID: uid, Username: "ALICE"}

	if err := s.changePassword(ctx, s.renderer(nil, term), identity, aud); err != nil {
		t.Fatalf("changePassword: %v", err)
	}

	// Assert: new password authenticates correctly.
	if _, err := auth.Authenticate(ctx, s.Store, "ALICE", "newpass"); err != nil {
		t.Errorf("authenticate with newpass: %v", err)
	}

	// Assert: old password no longer works.
	if _, err := auth.Authenticate(ctx, s.Store, "ALICE", "oldpass"); err == nil {
		t.Error("oldpass should no longer authenticate")
	}

	// Assert: audit trail includes password_self event.
	if !slices.ContainsFunc(rec.events, func(ev store.AuditEvent) bool {
		return ev.Kind == store.AuditPasswordSelf && ev.Username == "ALICE"
	}) {
		t.Errorf("no %s audit event for ALICE; got %v", store.AuditPasswordSelf, rec.kinds())
	}
}

func TestSelfMFAEnroll(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	ctx := context.Background()

	p := &fakePresenter{termType: "IBM-3278-2-E"}
	s, _ := newMFATestSession(t, p, &fakeBridger{})
	s.Authenticate = auth.Authenticate
	s.Throttle = newAuthThrottle()
	// newMFATestSession already sets s.Now to time.Unix(1_700_000_000, 0)

	// Override MFAGenerate to return the known secret so we can compute the code.
	s.MFAGenerate = func(_, _ string) (string, error) { return secret, nil }

	// Seed ALICE with a bcrypt password and NO MFA secret. newTestSession
	// pre-creates alice; SetPassword overwrites the hash for auth.Authenticate.
	hash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	uid, err := s.Store.CreateUser(ctx, "ALICE", hash)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.Store.SetPassword(ctx, uid, hash); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	// Compute the valid TOTP code for the known secret at s.Now.
	now := time.Unix(1_700_000_000, 0)
	step := uint64(now.Unix() / 30)
	validCode := codeForServer(t, secret, step)

	// Wire the fake step-up renderer: returns current password.
	fp := &fakeAdminPresenter{
		forms: []ui3270.FormAction{{Values: map[string]string{
			screens.FieldCurrentPassword: "pw",
		}}},
	}
	s.AdminRenderer = func(_ net.Conn, _ Term) ui3270.Renderer { return fp }

	// Queue the enroll confirm code on the fakePresenter.
	p.enrolls = []mfaResult{{code: validCode}}

	rec := &recordingAuditor{}
	aud := &auditTrail{auditor: rec}
	term := Term{Type: "IBM-3278-2", Rows: 24, Cols: 80}

	u, err := s.Store.GetUserByUsername(ctx, "ALICE")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}

	if err := s.selfMFAEnroll(ctx, nil, term, s.renderer(nil, term), u, aud); err != nil {
		t.Fatalf("selfMFAEnroll: %v", err)
	}

	// Assert: secret stored.
	u2, err := s.Store.GetUserByUsername(ctx, "ALICE")
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if u2.MFASecret == "" {
		t.Error("selfMFAEnroll: expected non-empty MFASecret after enrollment")
	}
	// Verify it decrypts to our known secret.
	pt, oerr := s.MFA.Open(u2.MFASecret)
	if oerr != nil || string(pt) != secret {
		t.Errorf("stored secret mismatch: plain=%q err=%v", pt, oerr)
	}

	// Assert: mfa_enrolled audited.
	if !slices.ContainsFunc(rec.events, func(ev store.AuditEvent) bool {
		return ev.Kind == store.AuditMFAEnrolled && ev.Username == "ALICE"
	}) {
		t.Errorf("no %s audit event for ALICE; got %v", store.AuditMFAEnrolled, rec.kinds())
	}
}

func TestSelfMFADisableClearsAndAudits(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	ctx := context.Background()

	p := &fakePresenter{termType: "IBM-3278-2-E"}
	s, _ := newMFATestSession(t, p, &fakeBridger{})
	s.Authenticate = auth.Authenticate
	s.Throttle = newAuthThrottle()

	// Seed ALICE with a bcrypt password AND a sealed MFA secret, mfa_required=false.
	// newTestSession pre-creates alice; SetPassword overwrites the hash.
	hash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	uid, err := s.Store.CreateUser(ctx, "ALICE", hash)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.Store.SetPassword(ctx, uid, hash); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	enc, err := s.MFA.Seal([]byte(secret))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if err := s.Store.StoreMFAEnrollment(ctx, uid, enc, "2026-01-01T00:00:00Z", 0); err != nil {
		t.Fatalf("StoreMFAEnrollment: %v", err)
	}
	// mfa_required stays false (default).

	// Wire the fake step-up renderer: returns current password.
	fp := &fakeAdminPresenter{
		forms: []ui3270.FormAction{{Values: map[string]string{
			screens.FieldCurrentPassword: "pw",
		}}},
	}
	s.AdminRenderer = func(_ net.Conn, _ Term) ui3270.Renderer { return fp }

	rec := &recordingAuditor{}
	aud := &auditTrail{auditor: rec}
	term := Term{Type: "IBM-3278-2", Rows: 24, Cols: 80}

	u, err := s.Store.GetUserByUsername(ctx, "ALICE")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if u.MFASecret == "" {
		t.Fatal("test setup: expected non-empty MFASecret before disable")
	}

	if err := s.selfMFADisable(ctx, s.renderer(nil, term), u, aud); err != nil {
		t.Fatalf("selfMFADisable: %v", err)
	}

	// Assert: secret cleared.
	u2, err := s.Store.GetUserByUsername(ctx, "ALICE")
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if u2.MFASecret != "" {
		t.Errorf("selfMFADisable: expected empty MFASecret, got %q", u2.MFASecret)
	}

	// Assert: mfa_cleared audited with Detail == "self-service".
	var found *store.AuditEvent
	for i := range rec.events {
		if rec.events[i].Kind == store.AuditMFACleared && rec.events[i].Username == "ALICE" {
			found = &rec.events[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no %s audit event for ALICE; got %v", store.AuditMFACleared, rec.kinds())
	}
	if found.Detail != "self-service" {
		t.Errorf("AuditMFACleared Detail = %q, want %q", found.Detail, "self-service")
	}
}

// TestSelfMFADisableBlockedWhenRequired locks the code-level defensive guard:
// an admin-required user can never self-disable, even if selfMFADisable is
// reached directly. No forms are queued, so if the guard failed to
// short-circuit and the step-up ran, fakeAdminPresenter.Form would panic —
// proving the guard returns BEFORE any step-up or mutation.
func TestSelfMFADisableBlockedWhenRequired(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	ctx := context.Background()

	p := &fakePresenter{termType: "IBM-3278-2-E"}
	s, _ := newMFATestSession(t, p, &fakeBridger{})
	s.Authenticate = auth.Authenticate
	s.Throttle = newAuthThrottle()

	hash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	uid, err := s.Store.CreateUser(ctx, "ALICE", hash)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.Store.SetPassword(ctx, uid, hash); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	enc, err := s.MFA.Seal([]byte(secret))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if err := s.Store.StoreMFAEnrollment(ctx, uid, enc, "2026-01-01T00:00:00Z", 0); err != nil {
		t.Fatalf("StoreMFAEnrollment: %v", err)
	}
	if err := s.Store.SetMFARequired(ctx, uid, true); err != nil {
		t.Fatalf("SetMFARequired: %v", err)
	}

	// No forms queued: a step-up would pop an empty queue and panic.
	fp := &fakeAdminPresenter{}
	s.AdminRenderer = func(_ net.Conn, _ Term) ui3270.Renderer { return fp }

	rec := &recordingAuditor{}
	aud := &auditTrail{auditor: rec}
	term := Term{Type: "IBM-3278-2", Rows: 24, Cols: 80}

	u, err := s.Store.GetUserByUsername(ctx, "ALICE")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}

	if err := s.selfMFADisable(ctx, s.renderer(nil, term), u, aud); err != nil {
		t.Fatalf("selfMFADisable: %v", err)
	}

	// The secret must remain and no mfa_cleared event may be recorded.
	u2, err := s.Store.GetUserByUsername(ctx, "ALICE")
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if u2.MFASecret == "" {
		t.Errorf("required user: secret was cleared; admin-required MFA must not be self-disabled")
	}
	for _, ev := range rec.events {
		if ev.Kind == store.AuditMFACleared {
			t.Errorf("required user: unexpected %s audit", store.AuditMFACleared)
		}
	}
}

func TestUserSettingsRowsAdaptive(t *testing.T) {
	cases := []struct {
		name     string
		mfaCfg   bool
		required bool
		enrolled bool
		want     []usAction
	}{
		{"mfa_off", false, false, false, []usAction{usChangePassword}},
		{"can_enroll", true, false, false, []usAction{usChangePassword, usEnroll}},
		{"enrolled_required", true, true, true, []usAction{usChangePassword, usReenroll}},
		{"enrolled_optional", true, false, true, []usAction{usChangePassword, usReenroll, usDisable}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := userSettingsActions(tc.mfaCfg, tc.required, tc.enrolled)
			if len(got) != len(tc.want) {
				t.Fatalf("actions=%d want %d (%v)", len(got), len(tc.want), got)
			}
			for i, a := range got {
				if a != tc.want[i] {
					t.Errorf("row %d action=%v want %v", i, a, tc.want[i])
				}
			}
		})
	}
}
