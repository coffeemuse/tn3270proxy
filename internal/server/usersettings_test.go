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

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/CoffeeMuse/tn3270proxy/internal/ui3270"
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

	// Seed ALICE with "oldpass" (bcrypt hashed).
	hash, err := auth.HashPassword("oldpass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	uid, err := s.Store.CreateUser(ctx, "ALICE", hash)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
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

func TestUserSettingsRowsAdaptive(t *testing.T) {
	cases := []struct {
		name       string
		mfaCfg     bool
		required   bool
		enrolled   bool
		wantLabels []string
	}{
		{"mfa_off", false, false, false, []string{"Change Password"}},
		{"can_enroll", true, false, false, []string{"Change Password", "Enroll in MFA"}},
		{"enrolled_required", true, true, true, []string{"Change Password", "Re-enroll MFA"}},
		{"enrolled_optional", true, false, true, []string{"Change Password", "Re-enroll MFA", "Disable MFA"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := userSettingsActions(tc.mfaCfg, tc.required, tc.enrolled)
			if len(got) != len(tc.wantLabels) {
				t.Fatalf("actions=%d want %d (%v)", len(got), len(tc.wantLabels), got)
			}
			for i, a := range got {
				if usActionLabel(a) != tc.wantLabels[i] {
					t.Errorf("row %d label=%q want %q", i, usActionLabel(a), tc.wantLabels[i])
				}
			}
		})
	}
}
