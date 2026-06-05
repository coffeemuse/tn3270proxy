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

func TestGeometryFormulas(t *testing.T) {
	cases := []struct {
		name                                  string
		g                                     Geometry
		help, errRow, legend, input, page, form int
	}{
		{"zero value", Geometry{}, 23, 21, 20, 19, 14, 9},
		{"MOD 2", Geometry{Rows: 24, Cols: 80}, 23, 21, 20, 19, 14, 9},
		{"MOD 3", Geometry{Rows: 32, Cols: 80}, 31, 29, 28, 27, 22, 13},
		{"MOD 4", Geometry{Rows: 43, Cols: 80}, 42, 40, 39, 38, 33, 18},
		{"MOD 5", Geometry{Rows: 27, Cols: 132}, 26, 24, 23, 22, 17, 10},
		{"sub-MOD 2 falls back", Geometry{Rows: 12, Cols: 40}, 23, 21, 20, 19, 14, 9},
		{"tall but narrow falls back", Geometry{Rows: 43, Cols: 40}, 23, 21, 20, 19, 14, 9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := []int{c.g.HelpRow(), c.g.ErrorRow(), c.g.LegendRow(), c.g.InputRow(), c.g.ListPageSize(), c.g.FormMaxFields()}
			want := []int{c.help, c.errRow, c.legend, c.input, c.page, c.form}
			for i, name := range []string{"HelpRow", "ErrorRow", "LegendRow", "InputRow", "ListPageSize", "FormMaxFields"} {
				if got[i] != want[i] {
					t.Errorf("%s() = %d, want %d", name, got[i], want[i])
				}
			}
		})
	}
}

func TestGeometryMenuCapacity(t *testing.T) {
	cases := []struct {
		g     Geometry
		admin bool
		want  int
	}{
		{Geometry{Rows: 24, Cols: 80}, false, 14}, // rows 4..17
		{Geometry{Rows: 24, Cols: 80}, true, 13},  // one row reserved for the A entry
		{Geometry{Rows: 32, Cols: 80}, false, 22},
		{Geometry{Rows: 43, Cols: 80}, true, 32},
		{Geometry{Rows: 27, Cols: 132}, false, 17},
		{Geometry{Rows: 27, Cols: 132}, true, 16},
		{Geometry{}, false, 14}, // zero value normalizes
	}
	for _, c := range cases {
		if got := c.g.MenuCapacity(c.admin); got != c.want {
			t.Errorf("%+v.MenuCapacity(%v) = %d, want %d", c.g, c.admin, got, c.want)
		}
	}
}

func TestNewsLinesPerPage(t *testing.T) {
	cases := []struct {
		name string
		geom Geometry
		want int
	}{
		{"mod2", Geometry{Rows: 24, Cols: 80}, 22},
		{"mod3", Geometry{Rows: 32, Cols: 80}, 30},
		{"mod4", Geometry{Rows: 43, Cols: 80}, 41},
		{"mod5", Geometry{Rows: 27, Cols: 132}, 25},
		{"zero normalizes to mod2", Geometry{}, 22},
		{"sub-mod2 normalizes to mod2", Geometry{Rows: 10, Cols: 40}, 22},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.geom.NewsLinesPerPage(); got != tc.want {
				t.Errorf("NewsLinesPerPage() = %d, want %d", got, tc.want)
			}
		})
	}
}
