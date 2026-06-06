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

package screens

import "testing"

func TestUserSettingsScreen(t *testing.T) {
	rows := []UserSettingsRow{{Key: "1", Label: "Change Password"}, {Key: "2", Label: "Enroll in MFA"}}
	screen, cur := UserSettingsScreen(Geometry{}, "ALICE", rows, "")
	if _, ok := fieldByName(screen, FieldUSOption); !ok {
		t.Fatalf("missing %q field", FieldUSOption)
	}
	if !screenContains(screen, "Change Password") || !screenContains(screen, "Enroll in MFA") {
		t.Errorf("rows not rendered: %+v", screen)
	}
	opt, _ := fieldByName(screen, FieldUSOption)
	if cur.Row != opt.Row || cur.Col != opt.Col+1 {
		t.Errorf("cursor = %v, want one past the option field at (%d,%d)", cur, opt.Row, opt.Col)
	}
}
