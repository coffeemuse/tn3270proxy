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

import (
	"testing"

	"github.com/racingmars/go3270"
)

func TestUserSettingsTriColor(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	rows := []UserSettingsRow{
		{Key: "1", Name: "Password", Description: "Change your sign-on password"},
		{Key: "2", Name: "MFA", Description: "Enroll in multi-factor authentication"},
	}
	screen, cur := UserSettingsScreen(g, "ROBERT", rows, "")

	key, ok := fieldByContent(screen, "  1")
	if !ok || key.Color != go3270.White || !key.Intense {
		t.Errorf("key = %+v ok=%v, want white intense", key, ok)
	}
	name, ok := fieldByContent(screen, "Password")
	if !ok || name.Col != 5 || name.Color != go3270.Turquoise {
		t.Errorf("name = %+v ok=%v, want col 5 turquoise", name, ok)
	}
	desc, ok := fieldByContent(screen, "Change your sign-on password")
	if !ok || desc.Col != 15 || desc.Color != go3270.Green {
		t.Errorf("desc = %+v ok=%v, want col 15 green", desc, ok)
	}
	opt, ok := fieldByName(screen, FieldUSOption)
	if !ok || opt.Row != 1 {
		t.Errorf("option field row = %d ok=%v, want 1", opt.Row, ok)
	}
	if cur.Row != 1 || cur.Col != 15 {
		t.Errorf("cursor = %+v, want (1,15)", cur)
	}
}
