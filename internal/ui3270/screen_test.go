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
	_, cur := buildListScreen(24, 80, ListView{Rows: []string{"alice", "bob"}})
	// first CMD field is attribute byte at (4,2); input one col right.
	if cur != (Cursor{Row: 4, Col: 3}) {
		t.Errorf("cursor = %+v, want {4,3}", cur)
	}
}

func TestBuildListScreenEmptyHomes(t *testing.T) {
	_, cur := buildListScreen(24, 80, ListView{})
	if cur != (Cursor{Row: 0, Col: 0}) {
		t.Errorf("cursor = %+v, want {0,0}", cur)
	}
}

func TestBuildFormScreenCursor(t *testing.T) {
	_, cur := buildFormScreen(24, 80, FormView{Fields: []FormField{{Name: "x", Length: 8}}})
	// first input attribute byte at (3,16); input one col right.
	if cur != (Cursor{Row: 3, Col: 17}) {
		t.Errorf("cursor = %+v, want {3,17}", cur)
	}
}
