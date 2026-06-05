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

package ui3270

import "testing"

func TestBuildListScreenCursorPopulated(t *testing.T) {
	_, cur := buildListScreen(24, ListView{Rows: []string{"alice", "bob"}})
	// first CMD field is attribute byte at (4,2); input one col right.
	if cur != (Cursor{Row: 4, Col: 3}) {
		t.Errorf("cursor = %+v, want {4,3}", cur)
	}
}

func TestBuildListScreenEmptyHomes(t *testing.T) {
	_, cur := buildListScreen(24, ListView{})
	if cur != (Cursor{Row: 0, Col: 0}) {
		t.Errorf("cursor = %+v, want {0,0}", cur)
	}
}

func TestBuildFormScreenCursor(t *testing.T) {
	_, cur := buildFormScreen(24, FormView{Fields: []FormField{{Name: "x", Length: 8}}})
	// first input attribute byte at (3,16); input one col right.
	if cur != (Cursor{Row: 3, Col: 17}) {
		t.Errorf("cursor = %+v, want {3,17}", cur)
	}
}

func TestBuildFormScreenReadOnlyField(t *testing.T) {
	screen, cur := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "username", Label: "User", Value: "RJLAWREN", ReadOnly: true},
		{Name: "fullname", Label: "Name", Length: 40},
	}})
	// Cursor skips the read-only field and homes to the first editable input.
	// Editable field is the 2nd row: attribute byte at (5,16), input one col right.
	if cur != (Cursor{Row: 5, Col: 17}) {
		t.Errorf("cursor = %+v, want {5,17}", cur)
	}
	// The read-only value renders as static content with no writable input field.
	var sawStatic, sawWritableUsername bool
	for _, f := range screen {
		if f.Content == "RJLAWREN" && !f.Write {
			sawStatic = true
		}
		if f.Name == "username" && f.Write {
			sawWritableUsername = true
		}
	}
	if !sawStatic {
		t.Errorf("read-only value not rendered as static content")
	}
	if sawWritableUsername {
		t.Errorf("read-only field must not be a writable input")
	}
}
