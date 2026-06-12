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
	"fmt"
	"testing"

	"github.com/racingmars/go3270"
)

// helpTestLines yields n distinct lines; zero-padded so "LINE01" can never
// substring-match "LINE10".
func helpTestLines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("LINE%02d CONTENT", i+1)
	}
	return out
}

func TestHelpPageBounds(t *testing.T) {
	g := DefaultGeometry // HelpPageSize 20 on MOD 2
	tests := []struct {
		name                         string
		total, page                  int
		wantPage, wantStart, wantEnd int
		wantInd                      string
	}{
		{"empty", 0, 0, 0, 0, 0, "PAGE 1 OF 1"},
		{"single page", 5, 0, 0, 0, 5, "PAGE 1 OF 1"},
		{"negative clamps", 5, -3, 0, 0, 5, "PAGE 1 OF 1"},
		{"second page", 26, 1, 1, 20, 26, "PAGE 2 OF 2"},
		{"beyond end clamps", 26, 9, 1, 20, 26, "PAGE 2 OF 2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page, start, end, ind := HelpPageBounds(g, tc.total, tc.page)
			if page != tc.wantPage || start != tc.wantStart || end != tc.wantEnd || ind != tc.wantInd {
				t.Errorf("got (%d,%d,%d,%q), want (%d,%d,%d,%q)",
					page, start, end, ind, tc.wantPage, tc.wantStart, tc.wantEnd, tc.wantInd)
			}
		})
	}
}

func TestHelpScreenTitleIndicatorAndHelpRow(t *testing.T) {
	screen, _ := HelpScreen(DefaultGeometry, "SERVICE MENU HELP", helpTestLines(3), 0)
	for _, want := range []string{"SERVICE MENU HELP", "PAGE 1 OF 1", "PF3=Return", "PF7=PgUp", "PF8=PgDn"} {
		if !screenContains(screen, want) {
			t.Errorf("help screen missing %q", want)
		}
	}
}

func TestHelpScreenPagesWindowAndColor(t *testing.T) {
	lines := helpTestLines(26) // 2 pages at MOD 2's 20-line page size
	p1, _ := HelpScreen(DefaultGeometry, "T", lines, 0)
	if !screenContains(p1, "LINE01 CONTENT") || screenContains(p1, "LINE21 CONTENT") {
		t.Error("page 1 should show lines 1..20 only")
	}
	if !screenContains(p1, "PAGE 1 OF 2") {
		t.Error("page 1 indicator wrong")
	}
	p2, _ := HelpScreen(DefaultGeometry, "T", lines, 1)
	if !screenContains(p2, "LINE21 CONTENT") || screenContains(p2, "LINE01 CONTENT") {
		t.Error("page 2 should show lines 21..26 only")
	}
	f, ok := fieldByContent(p2, "LINE21 CONTENT")
	if !ok || f.Color != go3270.Green {
		t.Errorf("body line field = %+v, want green protected text", f)
	}
}

func TestHelpScreenCursorHomes(t *testing.T) {
	_, cur := HelpScreen(DefaultGeometry, "T", helpTestLines(2), 0)
	if cur != (Cursor{}) {
		t.Errorf("cursor = %+v, want home {0,0} (no input field)", cur)
	}
}

func TestHelpScreenTruncatesWideLines(t *testing.T) {
	wide := []string{fmt.Sprintf("%080d", 1)} // 80 runes, must cut to 79
	screen, _ := HelpScreen(DefaultGeometry, "T", wide, 0)
	f, ok := fieldByContent(screen, wide[0][:79])
	if !ok {
		t.Fatal("missing truncated body line")
	}
	if len(f.Content) != 79 {
		t.Errorf("line length = %d, want 79", len(f.Content))
	}
}
