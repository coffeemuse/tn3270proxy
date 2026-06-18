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

import (
	"fmt"
	"strings"
)

// normRows clamps a sub-MOD 2 row count to the 24-row default. (Cols is not a
// layout-math input — content stays within columns 0–79 at every width.)
func normRows(rows int) int {
	if rows < 24 {
		return 24
	}
	return rows
}

// Bottom-anchored layout rows (CLAUDE.md convention; reproduce the historical
// 24-row layout). 0-based; 24 rows = 0..23.
func helpRow(rows int) int   { return normRows(rows) - 1 } // 23 on MOD 2
func legendRow(rows int) int { return normRows(rows) - 4 } // 20

// Top-band layout rows (ISPF style guide §2): title at row 0, an optional command
// line at row 1, the message line at row 2, body from row 3. Fixed on every model
// (content stays within cols 0–79; ui3270 adapts rows only).
func messageRow() int { return 2 }
func bodyTopRow() int { return 3 }

// centerCol returns the start column to center an n-rune title within the 0–79
// content band, clamped to ≥0.
func centerCol(n int) int {
	c := (80 - n) / 2
	if c < 0 {
		return 0
	}
	return c
}

// listPageSize is how many data rows fit on a list screen (14 on MOD 2).
func listPageSize(rows int) int { return normRows(rows) - 10 }

// formMaxFields is how many labeled inputs fit on a form (9 on MOD 2).
func formMaxFields(rows int) int { return (normRows(rows)-8)/2 + 1 }

// Form label/input column geometry (GH #71). The label attribute byte sits at
// labelAttrCol (content one column right); the input attribute byte is computed
// from the longest label so label text can never overrun it.
const (
	labelAttrCol  = 2  // label field attribute byte; content begins at col 3
	labelGutter   = 1  // ≥1 blank column between label end and input attribute
	minInputCol   = 16 // floor: preserves the historical layout for ≤12-char labels
	minInputWidth = 16 // columns reserved right of the ceiling input attr (~15 usable data cols)
)

// formInputCol returns the input field's attribute-byte column for a form whose
// longest label is maxLabel runes. Floored at minInputCol so short-label forms
// (every form whose labels are ≤12 chars) compute the historical col 16 and
// render byte-identically; clamped to a ceiling that preserves minInputWidth
// input columns even for a pathological label.
func formInputCol(maxLabel int) int {
	col := labelAttrCol + 1 + maxLabel + labelGutter
	col = max(col, minInputCol)
	col = min(col, 79-minInputWidth)
	return col
}

// formLabelMax returns the longest label content (in runes) that fits before the
// input attribute byte at inputCol. In the normal case this equals the form's
// longest label (a no-op); it only bites when inputCol hit the ceiling.
func formLabelMax(inputCol int) int {
	return inputCol - labelGutter - labelAttrCol - 1
}

// truncRunes returns s limited to at most n runes (n<0 ⇒ empty).
func truncRunes(s string, n int) string {
	if n < 0 {
		n = 0
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// pageBounds clamps page to the data and returns slice bounds + row indicator.
func pageBounds(page, total, rows int) (clamped, start, end int, info string) {
	if total == 0 {
		return 0, 0, 0, "ROW 0 OF 0"
	}
	size := listPageSize(rows)
	maxPage := (total - 1) / size
	if page > maxPage {
		page = maxPage
	}
	if page < 0 {
		page = 0
	}
	start = page * size
	end = min(start+size, total)
	return page, start, end, fmt.Sprintf("ROW %d TO %d OF %d", start+1, end, total)
}

// Compact-layout geometry (FormView.Compact). The body starts at row 2 (the
// message line moves to the bottom); SameRow fields and section banners use
// fixed columns. Content stays within cols 0-79.
const (
	compactBodyTopRow  = 2  // first content row in compact mode (row 2 is freed up)
	sameRowLabelCol    = 40 // second-column label attribute byte for a SameRow field
	sameRowInputCol    = 54 // second-column value attribute byte for a SameRow field
	sameRowLabelMax    = 12 // dot-leader width for a SameRow label (fits "MFA status :")
	sectionBannerWidth = 69 // banner content width: cols 3..71 within the 0-79 band
)

// compactMessageRow is the red message line row in compact mode (just above the
// PF-key help line, since row 2 now carries content). 22 on MOD 2.
func compactMessageRow(rows int) int { return helpRow(rows) - 1 }

// sectionBanner renders an ISPF-style group header, e.g.
// "--- Identity ----------------------------------------------------------",
// padded with trailing dashes to sectionBannerWidth runes.
func sectionBanner(name string) string {
	prefix := "--- " + name + " "
	if len([]rune(prefix)) >= sectionBannerWidth {
		return string([]rune(prefix)[:sectionBannerWidth])
	}
	return prefix + strings.Repeat("-", sectionBannerWidth-len([]rune(prefix)))
}
