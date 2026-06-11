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
	if !screenContains(screen, "Audit") {
		t.Errorf("AdminMenuScreen missing Audit entry keyword")
	}
	if !screenContains(screen, "Browse the audit trail") {
		t.Errorf("AdminMenuScreen missing Audit description")
	}
}

func TestAdminMenuTriColor(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, cur := AdminMenuScreen(g, "")

	key, ok := fieldByContent(screen, "  1")
	if !ok || key.Color != go3270.White || !key.Intense {
		t.Errorf("option key = %+v ok=%v, want white intense", key, ok)
	}
	name, ok := fieldByContent(screen, "Users")
	if !ok || name.Col != 6 || name.Color != go3270.Turquoise {
		t.Errorf("option name = %+v ok=%v, want col 6 turquoise", name, ok)
	}
	desc, ok := fieldByContent(screen, "User accounts and group membership")
	if !ok || desc.Col != 17 || desc.Color != go3270.Green {
		t.Errorf("option desc = %+v ok=%v, want col 17 green", desc, ok)
	}
	for _, kw := range []string{"Sysparms", "Networks", "Audit"} {
		if _, ok := fieldByContent(screen, kw); !ok {
			t.Errorf("missing ISPF keyword %q", kw)
		}
	}
	opt, _ := fieldByName(screen, FieldOption)
	if opt.Row != 1 {
		t.Errorf("option input row = %d, want 1", opt.Row)
	}
	if cur.Row != 1 || cur.Col != 15 {
		t.Errorf("cursor = %+v, want (1,15)", cur)
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

func TestAdminMenuScreen_HasSessionsOption(t *testing.T) {
	screen, _ := AdminMenuScreen(Geometry{Rows: 24, Cols: 80}, "")
	var sawKey, sawName bool
	for _, f := range screen {
		if f.Content == "  7" {
			sawKey = true
		}
		if f.Content == "Sessions" {
			sawName = true
		}
	}
	if !sawKey || !sawName {
		t.Errorf("admin menu missing option 7 Sessions (key=%v name=%v)", sawKey, sawName)
	}
}

func TestAdminMenuHasDocumentsOption(t *testing.T) {
	screen, _ := AdminMenuScreen(DefaultGeometry, "")
	found := false
	for _, f := range screen {
		if strings.Contains(f.Content, "Documents") {
			found = true
		}
	}
	if !found {
		t.Error("admin menu missing Documents option")
	}
}
