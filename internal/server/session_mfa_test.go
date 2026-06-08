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
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
)

// TestMFAGateSecretFirst asserts the headline invariant of GH #63:
// the gate is secret-first, not mfa_required-gated.
//
//   - optin_secret_not_required: secret stored, required=false → VerifyMFA called
//   - required_and_enrolled:     secret stored, required=true  → VerifyMFA called
//   - required_pending:          no secret,     required=true  → EnrollMFA called
//   - neither:                   no secret,     required=false → no MFA screen
//
// The critical regression case is optin_secret_not_required: the old gate
// returned early on !u.MFARequired, so a voluntarily-enrolled user was never
// challenged.
func TestMFAGateSecretFirst(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_700_000_000, 0)
	step := uint64(now.Unix() / 30)
	good, err := hotp.GenerateCodeCustom(secret, step, hotp.ValidateOpts{
		Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		required   bool
		enrolled   bool
		wantVerify bool
		wantEnroll bool
	}{
		{"optin_secret_not_required", false, true, true, false},
		{"required_and_enrolled", true, true, true, false},
		{"required_pending", true, false, false, true},
		{"neither", false, false, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Build the appropriate fake presenter queues.
			// For verify cases: queue one good code so the session proceeds to the menu.
			// For enroll cases: queue one good confirm code so enrollment completes.
			// For neither: no MFA queues needed; the gate should not touch the presenter.
			var p *fakePresenter
			switch {
			case tc.wantVerify:
				p = &fakePresenter{
					termType:  "IBM-3278-2-E",
					verifies:  []mfaResult{{code: good}},
					logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
					menuPicks: []menuResult{{quit: true}},
				}
			case tc.wantEnroll:
				p = &fakePresenter{
					termType:  "IBM-3278-2-E",
					enrolls:   []mfaResult{{code: good}},
					logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
					menuPicks: []menuResult{{quit: true}},
				}
			default: // neither
				p = &fakePresenter{
					termType:  "IBM-3278-2-E",
					logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
					menuPicks: []menuResult{{quit: true}},
				}
			}

			b := &fakeBridger{}
			s, st := newMFATestSession(t, p, b)
			// Override MFAGenerate so enroll path uses our known secret.
			s.MFAGenerate = func(_, _ string) (string, error) { return secret, nil }

			ctx := context.Background()
			uid, _ := st.CreateUser(ctx, "alice", "x")

			if tc.required {
				st.SetMFARequired(ctx, uid, true)
			}
			if tc.enrolled {
				enc, err := s.MFA.Seal([]byte(secret))
				if err != nil {
					t.Fatal(err)
				}
				st.StoreMFAEnrollment(ctx, uid, enc, "2026-01-01T00:00:00Z", 0)
			}

			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			s.Run(client)

			// Assert which MFA presenter method was (or was not) called.
			verifyWasCalled := len(p.verifyErrors) > 0
			enrollWasCalled := len(p.enrollErrors) > 0

			if verifyWasCalled != tc.wantVerify {
				t.Errorf("VerifyMFA called=%v, want %v", verifyWasCalled, tc.wantVerify)
			}
			if enrollWasCalled != tc.wantEnroll {
				t.Errorf("EnrollMFA called=%v, want %v", enrollWasCalled, tc.wantEnroll)
			}

			// In all cases the session must have reached the menu (proceed=true path).
			if len(p.menuPicks) != 0 {
				t.Errorf("menu was never reached (menuPicks remaining: %d)", len(p.menuPicks))
			}
		})
	}
}

// TestMFAGateLockedSkipsEnrollment asserts that a user_settings_locked account
// never enters the enrollment branch (forced or otherwise), but a stored secret
// is still verified at login.
func TestMFAGateLockedSkipsEnrollment(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_700_000_000, 0)
	step := uint64(now.Unix() / 30)
	good, err := hotp.GenerateCodeCustom(secret, step, hotp.ValidateOpts{
		Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("locked_required_no_secret_no_enroll", func(t *testing.T) {
		p := &fakePresenter{
			termType:  "IBM-3278-2-E",
			logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
			menuPicks: []menuResult{{quit: true}},
		}
		b := &fakeBridger{}
		s, st := newMFATestSession(t, p, b)
		ctx := context.Background()
		uid, _ := st.CreateUser(ctx, "alice", "x")
		st.SetMFARequired(ctx, uid, true)
		st.SetUserSettingsLocked(ctx, uid, true)

		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		s.Run(client)

		if len(p.enrollErrors) > 0 {
			t.Error("EnrollMFA must not be called for a locked account")
		}
		if len(p.menuPicks) != 0 {
			t.Error("session should have reached the menu (no MFA prompt)")
		}
	})

	t.Run("locked_with_secret_still_verifies", func(t *testing.T) {
		p := &fakePresenter{
			termType:  "IBM-3278-2-E",
			verifies:  []mfaResult{{code: good}},
			logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
			menuPicks: []menuResult{{quit: true}},
		}
		b := &fakeBridger{}
		s, st := newMFATestSession(t, p, b)
		ctx := context.Background()
		uid, _ := st.CreateUser(ctx, "alice", "x")
		st.SetUserSettingsLocked(ctx, uid, true)
		enc, err := s.MFA.Seal([]byte(secret))
		if err != nil {
			t.Fatal(err)
		}
		st.StoreMFAEnrollment(ctx, uid, enc, "2026-01-01T00:00:00Z", 0)

		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		s.Run(client)

		if len(p.verifyErrors) == 0 {
			t.Error("VerifyMFA must still be called for a locked, enrolled account")
		}
		if len(p.menuPicks) != 0 {
			t.Error("session should have reached the menu after verify")
		}
	})
}
