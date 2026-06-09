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

	"github.com/racingmars/go3270"
)

// newsMaxCols is the rightmost usable column for MOTD text. The file is a
// deliberately composed fixed-width banner; the author owns line breaks and
// indentation, and anything past column 79 is silently dropped (all MOD types,
// including MOD 5, truncate at 79).
const newsMaxCols = 79

// SplitBranding turns raw branding-file text into the lines the login screen
// renders. It mirrors PaginateNews's line handling but does not paginate: lines
// split on "\n" (a trailing "\r" is stripped) and truncate at column 79.
// Trailing whitespace-only lines are dropped (so a conventional EOF newline
// doesn't skew vertical centering); leading and interior blank lines are
// preserved (the author owns vertical spacing). Returns nil when the text is
// empty or whitespace-only.
func SplitBranding(raw string) []string {
	lines := strings.Split(raw, "\n")
	for i, ln := range lines {
		ln = strings.TrimSuffix(ln, "\r")
		if r := []rune(ln); len(r) > newsMaxCols {
			ln = string(r[:newsMaxCols])
		}
		lines[i] = ln
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}
	return lines
}

// PaginateNews turns raw MOTD file text into pages for NewsScreen. Lines split
// on "\n" (a trailing "\r" is stripped) and truncate at column 79. Trailing
// blank lines are dropped so a conventional EOF newline never yields a spurious
// blank page; leading and interior blank lines are preserved (the author owns
// spacing). Returns nil when the text is empty or whitespace-only — the caller
// treats nil as "nothing to show". Each page holds up to geom.NewsLinesPerPage()
// lines.
func PaginateNews(geom Geometry, raw string) [][]string {
	lines := strings.Split(raw, "\n")
	for i, ln := range lines {
		ln = strings.TrimSuffix(ln, "\r")
		if r := []rune(ln); len(r) > newsMaxCols {
			ln = string(r[:newsMaxCols])
		}
		lines[i] = ln
	}
	// Drop trailing whitespace-only lines (EOF newline / blank padding).
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}
	per := geom.NewsLinesPerPage()
	var pages [][]string
	for i := 0; i < len(lines); i += per {
		end := i + per
		if end > len(lines) {
			end = len(lines)
		}
		pages = append(pages, lines[i:end])
	}
	return pages
}

// NewsScreen renders one MOTD/NEWS page: each text line as a red, protected
// field at column 0, then the "***" page gate on the last row. There is no
// title row and no PF-key help row — a deliberate departure from the app's
// usual screen chrome, matching classic TSO/READY logon messages. The cursor
// homes to {0,0}; there is no input field. The caller (Presenter.News) drives
// it with go3270.HandleScreenAlt: AIDEnter advances, PA3/PF3 are silent no-ops.
func NewsScreen(geom Geometry, page []string) (go3270.Screen, go3270.Rules, Cursor) {
	screen := make(go3270.Screen, 0, len(page)+1)
	for i, line := range page {
		screen = append(screen, go3270.Field{
			Row: i, Col: 0, Color: go3270.Red, Content: line,
		})
	}
	screen = append(screen, go3270.Field{
		Row: geom.NewsLinesPerPage() + 1, Col: 0, Color: go3270.Red, Content: "***",
	})
	return screen, nil, Cursor{Row: 0, Col: 0}
}
