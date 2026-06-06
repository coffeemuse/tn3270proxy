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

import "testing"

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
