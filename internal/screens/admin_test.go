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
	"strings"
	"testing"

	"github.com/racingmars/go3270"
)

func screenContains(s go3270.Screen, sub string) bool {
	for _, f := range s {
		if strings.Contains(f.Content, sub) {
			return true
		}
	}
	return false
}

func TestAdminMenuScreenFields(t *testing.T) {
	screen, _ := AdminMenuScreen(DefaultGeometry, "boom")
	f, ok := fieldByName(screen, FieldOption)
	if !ok {
		t.Errorf("missing %q field", FieldOption)
	} else if !f.Write {
		t.Errorf("FieldOption must be writable")
	}
	f, ok = fieldByName(screen, FieldError)
	if !ok || f.Content != "boom" {
		t.Errorf("error field = %+v, ok=%v", f, ok)
	}
	// the three entity options are listed
	for _, want := range []string{"Users", "Groups", "Services"} {
		if !screenContains(screen, want) {
			t.Errorf("menu missing %q", want)
		}
	}
}

func TestAdminMenuScreen_HasAuditEntry(t *testing.T) {
	screen, _ := AdminMenuScreen(DefaultGeometry, "")
	var found bool
	for _, f := range screen {
		if f.Content == "6.  Audit Log" {
			found = true
		}
	}
	if !found {
		t.Errorf("AdminMenuScreen missing '6.  Audit Log' entry")
	}
}

func TestAdminMenuScreenCursor(t *testing.T) {
	screen, cur := AdminMenuScreen(DefaultGeometry, "")
	of, ok := fieldByName(screen, FieldOption)
	if !ok {
		t.Fatalf("missing %q field", FieldOption)
	}
	if want := cursorAt(of); cur != want {
		t.Errorf("admin menu cursor = %+v, want %+v (option field row %d col %d)", cur, want, of.Row, of.Col)
	}
}

