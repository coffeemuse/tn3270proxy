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

func TestMenuPageBounds(t *testing.T) {
	g := DefaultGeometry // MOD 2: capacity 18 non-admin, 17 admin
	cases := []struct {
		name                string
		total, page         int
		admin               bool
		wantPage, wantStart int
		wantEnd             int
		wantIndicator       string
	}{
		{"empty", 0, 0, false, 0, 0, 0, "ITEMS 0 OF 0"},
		{"empty ignores page", 0, 3, false, 0, 0, 0, "ITEMS 0 OF 0"},
		{"single page", 5, 0, false, 0, 0, 5, "ITEMS 1 TO 5 OF 5"},
		{"single page over-clamps", 5, 9, false, 0, 0, 5, "ITEMS 1 TO 5 OF 5"},
		{"two pages first", 22, 0, false, 0, 0, 18, "ITEMS 1 TO 18 OF 22"},
		{"two pages second", 22, 1, false, 1, 18, 22, "ITEMS 19 TO 22 OF 22"},
		{"over-clamp to last", 22, 9, false, 1, 18, 22, "ITEMS 19 TO 22 OF 22"},
		{"under-clamp to first", 22, -3, false, 0, 0, 18, "ITEMS 1 TO 18 OF 22"},
		{"admin smaller page", 22, 1, true, 1, 17, 22, "ITEMS 18 TO 22 OF 22"},
	}
	for _, c := range cases {
		gotPage, gotStart, gotEnd, gotInd := MenuPageBounds(g, c.total, c.admin, c.page)
		if gotPage != c.wantPage || gotStart != c.wantStart || gotEnd != c.wantEnd || gotInd != c.wantIndicator {
			t.Errorf("%s: MenuPageBounds(total=%d, admin=%v, page=%d) = (%d,%d,%d,%q), want (%d,%d,%d,%q)",
				c.name, c.total, c.admin, c.page,
				gotPage, gotStart, gotEnd, gotInd,
				c.wantPage, c.wantStart, c.wantEnd, c.wantIndicator)
		}
	}
}
