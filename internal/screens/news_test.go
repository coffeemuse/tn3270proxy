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
