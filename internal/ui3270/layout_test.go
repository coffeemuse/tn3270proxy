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

func TestPageBounds(t *testing.T) {
	cases := []struct {
		page, total, rows     int
		wantPage, wantS, wantE int
		wantInfo               string
	}{
		{0, 0, 24, 0, 0, 0, "ROW 0 OF 0"},
		{0, 3, 24, 0, 0, 3, "ROW 1 TO 3 OF 3"},
		{-1, 3, 24, 0, 0, 3, "ROW 1 TO 3 OF 3"},
		{0, 20, 24, 0, 0, 14, "ROW 1 TO 14 OF 20"},
		{1, 20, 24, 1, 14, 20, "ROW 15 TO 20 OF 20"},
		{5, 20, 24, 1, 14, 20, "ROW 15 TO 20 OF 20"},
		{0, 30, 32, 0, 0, 22, "ROW 1 TO 22 OF 30"},
		{1, 30, 32, 1, 22, 30, "ROW 23 TO 30 OF 30"},
	}
	for _, c := range cases {
		page, s, e, info := pageBounds(c.page, c.total, c.rows)
		if page != c.wantPage || s != c.wantS || e != c.wantE || info != c.wantInfo {
			t.Errorf("pageBounds(%d,%d,%d) = %d,%d,%d,%q want %d,%d,%d,%q",
				c.page, c.total, c.rows, page, s, e, info, c.wantPage, c.wantS, c.wantE, c.wantInfo)
		}
	}
}

func TestLayoutFormulas(t *testing.T) {
	if got := listPageSize(24); got != 14 {
		t.Errorf("listPageSize(24) = %d, want 14", got)
	}
	if got := formMaxFields(24); got != 9 {
		t.Errorf("formMaxFields(24) = %d, want 9", got)
	}
	if got := listPageSize(10); got != 14 {
		t.Errorf("listPageSize(10) = %d, want 14", got)
	}
}
