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
		name                     string
		g                        Geometry
		help, legend, page, form int
	}{
		{"zero value", Geometry{}, 23, 20, 14, 9},
		{"MOD 2", Geometry{Rows: 24, Cols: 80}, 23, 20, 14, 9},
		{"MOD 3", Geometry{Rows: 32, Cols: 80}, 31, 28, 22, 13},
		{"MOD 4", Geometry{Rows: 43, Cols: 80}, 42, 39, 33, 18},
		{"MOD 5", Geometry{Rows: 27, Cols: 132}, 26, 23, 17, 10},
		{"sub-MOD 2 falls back", Geometry{Rows: 12, Cols: 40}, 23, 20, 14, 9},
		{"tall but narrow falls back", Geometry{Rows: 43, Cols: 40}, 23, 20, 14, 9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := []int{c.g.HelpRow(), c.g.LegendRow(), c.g.ListPageSize(), c.g.FormMaxFields()}
			want := []int{c.help, c.legend, c.page, c.form}
			for i, name := range []string{"HelpRow", "LegendRow", "ListPageSize", "FormMaxFields"} {
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
		{Geometry{Rows: 24, Cols: 80}, false, 18}, // body rows 3..21 (no instruction line, GH #130); "0" reserved
		{Geometry{Rows: 24, Cols: 80}, true, 17},  // one more row reserved for the A entry
		{Geometry{Rows: 32, Cols: 80}, false, 26},
		{Geometry{Rows: 43, Cols: 80}, true, 36},
		{Geometry{Rows: 27, Cols: 132}, false, 21},
		{Geometry{Rows: 27, Cols: 132}, true, 20},
		{Geometry{}, false, 18}, // zero value normalizes
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

func TestStatusBlockCol(t *testing.T) {
	// Fixed at 60 on the 80-col layout: the service grid (desc col 17, cut 40)
	// ends at col 57, leaving a gutter before the block. The value is the
	// same on taller models because content is always within cols 0-79.
	for _, g := range []Geometry{DefaultGeometry, {Rows: 43, Cols: 80}, {}} {
		if got := g.StatusBlockCol(); got != 60 {
			t.Errorf("StatusBlockCol(%+v) = %d, want 60", g, got)
		}
	}
}

func TestTopBandHelpers(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	if g.TitleRow() != 0 {
		t.Errorf("TitleRow = %d, want 0", g.TitleRow())
	}
	if g.CommandRow() != 1 {
		t.Errorf("CommandRow = %d, want 1", g.CommandRow())
	}
	if g.MessageRow() != 2 {
		t.Errorf("MessageRow = %d, want 2", g.MessageRow())
	}
	if g.BodyTopRow() != 3 {
		t.Errorf("BodyTopRow = %d, want 3", g.BodyTopRow())
	}
	if g.BodyBottomRow() != 22 {
		t.Errorf("BodyBottomRow = %d, want 22", g.BodyBottomRow())
	}
}

func TestCenterCol(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	// "TN3270 GATEWAY MENU" is 19 runes → (80-19)/2 = 30.
	if got := g.CenterCol(19); got != 30 {
		t.Errorf("CenterCol(19) = %d, want 30", got)
	}
	// Over-wide text clamps to 0, never negative.
	if got := g.CenterCol(200); got != 0 {
		t.Errorf("CenterCol(200) = %d, want 0", got)
	}
	// Zero geometry normalizes to 24x80 before centering.
	if got := (Geometry{}).CenterCol(19); got != 30 {
		t.Errorf("zero-geom CenterCol(19) = %d, want 30", got)
	}
}

func TestHelpPageSize(t *testing.T) {
	if got := DefaultGeometry.HelpPageSize(); got != 20 {
		t.Errorf("MOD 2 help page size = %d, want 20", got)
	}
	if got := (Geometry{Rows: 43, Cols: 80}).HelpPageSize(); got != 39 {
		t.Errorf("MOD 4 help page size = %d, want 39", got)
	}
}

func TestLoginBrandingGeometry(t *testing.T) {
	cases := []struct {
		g          Geometry
		wantTop    int
		wantHeight int
	}{
		{Geometry{24, 80}, 4, 18}, // BodyBottomRow 22 -> height 22-4
		{Geometry{}, 4, 18},       // normalizes to 24x80
		{Geometry{32, 80}, 4, 26}, // BodyBottomRow 30 -> 26
		{Geometry{43, 80}, 4, 37}, // BodyBottomRow 41 -> 37
	}
	for _, c := range cases {
		if got := c.g.LoginBrandingTop(); got != c.wantTop {
			t.Errorf("%+v LoginBrandingTop = %d, want %d", c.g, got, c.wantTop)
		}
		if got := c.g.LoginBrandingHeight(); got != c.wantHeight {
			t.Errorf("%+v LoginBrandingHeight = %d, want %d", c.g, got, c.wantHeight)
		}
		// Region must sit strictly between the header band and the input row.
		if c.g.LoginBrandingTop()+c.g.LoginBrandingHeight() != c.g.BodyBottomRow() {
			t.Errorf("%+v region bottom should be input row %d", c.g, c.g.BodyBottomRow())
		}
	}
}
