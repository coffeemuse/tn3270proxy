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

	"github.com/racingmars/go3270"
)

// HelpPageBounds clamps page against total lines and the per-page help
// capacity, returning the clamped page, the [start,end) slice bounds for that
// page, and the "PAGE x OF y" indicator. It is the single source of help
// paging math, mirroring MenuPageBounds: HelpScreen uses it to render the
// window, and the viewer loop uses it to clamp its stored page so PF7/PF8 are
// no-ops at the ends.
func HelpPageBounds(geom Geometry, total, page int) (clamped, start, end int, indicator string) {
	if total == 0 {
		return 0, 0, 0, "PAGE 1 OF 1"
	}
	size := geom.HelpPageSize()
	maxPage := (total - 1) / size
	if page > maxPage {
		page = maxPage
	}
	if page < 0 {
		page = 0
	}
	start = page * size
	end = min(start+size, total)
	return page, start, end, fmt.Sprintf("PAGE %d OF %d", page+1, maxPage+1)
}

// HelpScreen renders one page of a read-only help document as a three-band
// ISPF screen (docs/dev/ispf-style-guide.md): centered title and right-aligned
// PAGE x OF y indicator on the title row, text lines (protected, green,
// truncated at column 79) filling the body band, and the PF-key help on the
// last row. This is deliberately NOT the chrome-less MOTD/news pager — help is
// an ISPF-layer screen. There is no input field; the cursor homes to {0,0}
// (the empty-admin-list precedent). The caller drives paging with PF7/PF8;
// out-of-range pages clamp (see HelpPageBounds).
func HelpScreen(geom Geometry, title string, lines []string, page int) (go3270.Screen, Cursor) {
	_, start, end, indicator := HelpPageBounds(geom, len(lines), page)
	// Right-align the indicator so its content ends at col 79 (a Field's Col
	// is the attribute byte, so content starts at Col+1) — the menu convention.
	indicatorCol := max(79-len(indicator), 0)
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len(title)), Color: go3270.White, Intense: true, Content: title},
		{Row: geom.TitleRow(), Col: indicatorCol, Color: go3270.Turquoise, Content: indicator},
	}
	row := geom.BodyTopRow()
	for i := start; i < end; i++ {
		screen = append(screen, go3270.Field{
			Row: row, Col: 0, Color: go3270.Green, Content: truncateRunes(lines[i], 79),
		})
		row++
	}
	screen = append(screen, go3270.Field{
		Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise,
		Content: "PF3=Return   PF7=PgUp  PF8=PgDn",
	})
	return screen, Cursor{Row: 0, Col: 0}
}
