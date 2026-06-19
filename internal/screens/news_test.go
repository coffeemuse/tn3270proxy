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

func TestPaginateNewsEmptyAndWhitespace(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80}
	for _, raw := range []string{"", "   ", "\n\n", "  \n \t \n"} {
		if pages := PaginateNews(mod2, raw); pages != nil {
			t.Errorf("PaginateNews(%q) = %v, want nil", raw, pages)
		}
	}
}

func TestPaginateNewsSinglePage(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80}
	pages := PaginateNews(mod2, "line one\nline two\nline three")
	if len(pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(pages))
	}
	want := []string{"line one", "line two", "line three"}
	if len(pages[0]) != len(want) {
		t.Fatalf("page has %d lines, want %d", len(pages[0]), len(want))
	}
	for i, w := range want {
		if pages[0][i] != w {
			t.Errorf("line %d = %q, want %q", i, pages[0][i], w)
		}
	}
}

func TestPaginateNewsStripsTrailingBlankLinesAndCR(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80}
	pages := PaginateNews(mod2, "alpha\r\nbeta\r\n\r\n")
	if len(pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(pages))
	}
	if len(pages[0]) != 2 || pages[0][0] != "alpha" || pages[0][1] != "beta" {
		t.Errorf("page = %#v, want [alpha beta]", pages[0])
	}
}

func TestPaginateNewsPreservesInteriorBlankLines(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80}
	pages := PaginateNews(mod2, "top\n\nbottom")
	if len(pages) != 1 || len(pages[0]) != 3 || pages[0][1] != "" {
		t.Errorf("page = %#v, want [top, \"\", bottom]", pages[0])
	}
}

func TestPaginateNewsTruncatesAt79(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80}
	long := strings.Repeat("X", 100)
	pages := PaginateNews(mod2, long)
	if len(pages) != 1 || len(pages[0]) != 1 {
		t.Fatalf("got pages %#v, want one line", pages)
	}
	if got := pages[0][0]; len([]rune(got)) != 79 {
		t.Errorf("truncated line length = %d, want 79", len([]rune(got)))
	}
}

func TestPaginateNewsSplitsAcrossPages(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80} // 22 lines/page
	var b strings.Builder
	for i := 0; i < 45; i++ { // 45 lines -> 22 + 22 + 1
		b.WriteString("L\n")
	}
	pages := PaginateNews(mod2, b.String())
	if len(pages) != 3 {
		t.Fatalf("got %d pages, want 3", len(pages))
	}
	if len(pages[0]) != 22 || len(pages[1]) != 22 || len(pages[2]) != 1 {
		t.Errorf("page sizes = %d/%d/%d, want 22/22/1",
			len(pages[0]), len(pages[1]), len(pages[2]))
	}
}

func TestPaginateNewsExactFitOnePage(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80} // 22 lines/page
	var b strings.Builder
	for i := 0; i < 22; i++ {
		b.WriteString("L\n")
	}
	pages := PaginateNews(mod2, b.String())
	if len(pages) != 1 || len(pages[0]) != 22 {
		t.Errorf("got %d pages (first %d lines), want 1 page of 22",
			len(pages), len(pages[0]))
	}
}

func TestNewsScreenFieldsRedProtectedWithGate(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80}
	page := []string{"NEWS LINE A", "NEWS LINE B"}
	screen, rules, cur := NewsScreen(mod2, page)

	if rules != nil {
		t.Errorf("rules = %v, want nil (no input fields)", rules)
	}
	if cur != (Cursor{Row: 0, Col: 0}) {
		t.Errorf("cursor = %+v, want home {0,0}", cur)
	}
	// One field per line + one "***" gate field.
	if len(screen) != len(page)+1 {
		t.Fatalf("screen has %d fields, want %d", len(screen), len(page)+1)
	}
	for i := 0; i < len(page); i++ {
		f := screen[i]
		if f.Row != i || f.Col != 0 {
			t.Errorf("line %d at (%d,%d), want (%d,0)", i, f.Row, f.Col, i)
		}
		if f.Content != page[i] {
			t.Errorf("line %d content = %q, want %q", i, f.Content, page[i])
		}
		if f.Color != go3270.Red {
			t.Errorf("line %d color = %v, want Red", i, f.Color)
		}
		if f.Write {
			t.Errorf("line %d is writable; MOTD text must be protected", i)
		}
	}
	gate := screen[len(screen)-1]
	if gate.Content != "***" {
		t.Errorf("gate content = %q, want ***", gate.Content)
	}
	if gate.Row != len(page)+1 { // one blank row below the last text line
		t.Errorf("gate row = %d, want %d", gate.Row, len(page)+1)
	}
	if gate.Color != go3270.Red || gate.Write {
		t.Errorf("gate field = %+v, want red protected", gate)
	}
}

func TestNewsScreenGateFloatsBelowShortPage(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80}
	screen, _, _ := NewsScreen(mod2, []string{"only one line"})
	gate := screen[len(screen)-1]
	// One text line at row 0, blank row 1, gate at row 2 — not pinned to the
	// bottom of the display (classic TSO floats it just below the content).
	if gate.Row != 2 {
		t.Errorf("gate row on short page = %d, want 2 (one blank line below content)", gate.Row)
	}
}

func TestNewsScreenGateStaysOnLastRowForFullPage(t *testing.T) {
	mod2 := Geometry{Rows: 24, Cols: 80} // 22 lines/page
	page := make([]string, mod2.NewsLinesPerPage())
	for i := range page {
		page[i] = "L"
	}
	screen, _, _ := NewsScreen(mod2, page)
	gate := screen[len(screen)-1]
	// A full page leaves exactly one blank row, so the gate lands on the last
	// row (23 on MOD 2) — unchanged from the old fixed placement.
	if gate.Row != 23 {
		t.Errorf("gate row on full page = %d, want 23 (last row)", gate.Row)
	}
}

func TestSplitBranding(t *testing.T) {
	if got := SplitBranding(""); got != nil {
		t.Errorf("empty input = %v, want nil", got)
	}
	if got := SplitBranding("  \n\t\n"); got != nil {
		t.Errorf("whitespace-only = %v, want nil", got)
	}
	got := SplitBranding("ALPHA\r\nBETA\n\n")
	want := []string{"ALPHA", "BETA"} // trailing blank lines trimmed, \r stripped
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
	// Lines hard-cut at column 79 (80 runes -> 79).
	long := SplitBranding(strings.Repeat("X", 100))
	if len(long) != 1 || len([]rune(long[0])) != 79 {
		t.Errorf("long line len = %d, want 79", len([]rune(long[0])))
	}
	// Interior blank lines are preserved (author owns vertical spacing).
	if got := SplitBranding("A\n\nB"); len(got) != 3 || got[1] != "" {
		t.Errorf("interior blank not preserved: %v", got)
	}
}
