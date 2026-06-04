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

import "fmt"

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
func errorRow(rows int) int  { return normRows(rows) - 3 } // 21
func legendRow(rows int) int { return normRows(rows) - 4 } // 20

// listPageSize is how many data rows fit on a list screen (14 on MOD 2).
func listPageSize(rows int) int { return normRows(rows) - 10 }

// formMaxFields is how many labeled inputs fit on a form (9 on MOD 2).
func formMaxFields(rows int) int { return (normRows(rows)-8)/2 + 1 }

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
