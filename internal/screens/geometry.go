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

// Geometry is the client terminal's screen size in rows × columns. The zero
// value — and anything smaller than the 24×80 MOD 2 default in either
// dimension — normalizes to 24×80, so callers may pass an unknown geometry
// safely. Content stays within columns 0–79 regardless of width (rows-only
// adaptation; see the larger-terminals design spec).
type Geometry struct{ Rows, Cols int }

// DefaultGeometry is the 24×80 MOD 2 screen every 3270 client supports.
// It is the safe fallback value; callers must not mutate it.
var DefaultGeometry = Geometry{Rows: 24, Cols: 80}

// norm clamps zero/unknown/sub-MOD 2 dimensions to the 24×80 default.
func (g Geometry) norm() Geometry {
	if g.Rows < 24 || g.Cols < 80 {
		return DefaultGeometry
	}
	return g
}

// Bottom-anchored layout rows (CLAUDE.md layout convention: title on row 0,
// PF help on the last row, error line just above the action line). Every
// formula reproduces the historical fixed layout at 24 rows.

// HelpRow is the PF-key help line (last row; 23 on MOD 2).
func (g Geometry) HelpRow() int { return g.norm().Rows - 1 }

// LegendRow is the admin-list line-command legend (20 on MOD 2).
func (g Geometry) LegendRow() int { return g.norm().Rows - 4 }

// ListPageSize is how many data rows fit on an admin list screen
// (rows 4 .. Rows-7; 14 on MOD 2).
func (g Geometry) ListPageSize() int { return g.norm().Rows - 10 }

// FormMaxFields is how many labeled inputs fit on an admin form
// (rows 3, 5, 7, …, two apart, ending at Rows-5 or Rows-6 by parity;
// 9 on MOD 2).
func (g Geometry) FormMaxFields() int { return (g.norm().Rows-8)/2 + 1 }

// menuBottomRow is the menu's effective bottom content row: one above
// BodyBottomRow, leaving a blank separator line above the PF-key legend
// (menu-only; other screens use BodyBottomRow directly).
func (g Geometry) menuBottomRow() int { return g.BodyBottomRow() - 1 }

// MenuCapacity is how many service lines fit on one menu page: body rows
// BodyTopRow+1 .. menuBottomRow (row BodyTopRow is the instruction line), minus
// one for the "0 User Settings" meta row (reserved even when hidden by
// settingsLocked, so paging math is stable) and one more for the admin "A" row.
// The blank separator above the PF legend is excluded via menuBottomRow.
func (g Geometry) MenuCapacity(admin bool) int {
	n := g.menuBottomRow() - (g.BodyTopRow() + 1) + 1 - 1 // body rows minus the "0" meta row
	if admin {
		n--
	}
	return n
}

// NewsLinesPerPage is how many MOTD/NEWS text lines fit on one page: every row
// except the bottom row reserved for the "***" page gate and the blank row
// above it (22 on MOD 2). The "***" sits on row NewsLinesPerPage()+1 (the last
// row); see NewsScreen.
func (g Geometry) NewsLinesPerPage() int { return g.norm().Rows - 2 }

// Top-band layout rows (ISPF style guide §2). The title/command/message band is
// fixed at rows 0–2; the body fills row 3 down to BodyBottomRow; PF-key help
// stays on the last row. See docs/ispf-style-guide.md.

// TitleRow is the centered panel-title row.
func (g Geometry) TitleRow() int { return 0 }

// CommandRow is the "Option ===>" / "Command ===>" command line (menus & lists).
func (g Geometry) CommandRow() int { return 1 }

// MessageRow is the red message line, directly under the command line.
func (g Geometry) MessageRow() int { return 2 }

// BodyTopRow is the first body row (column headings on a list).
func (g Geometry) BodyTopRow() int { return 3 }

// BodyBottomRow is the last usable body row — one above the PF-key help row.
func (g Geometry) BodyBottomRow() int { return g.norm().Rows - 2 }

// CenterCol returns the starting column to center an n-rune string within
// columns 0..Cols-1, clamped to 0 (never negative).
func (g Geometry) CenterCol(n int) int {
	c := (g.norm().Cols - n) / 2
	if c < 0 {
		return 0
	}
	return c
}

// StatusBlockCol is the left column of the menu's right-hand status block
// (the per-session User ID / Date / Time / Terminal / System ID / Release
// panel). It is fixed at 60: the service grid (number col 0, name col 6,
// description col 17 hard-cut to 40) ends at col 57, and the block's 10-char
// labels + 7-rune values fit within cols 60-78. Content is always within cols
// 0-79 (rows-only adaptation), so this does not widen on taller models.
func (g Geometry) StatusBlockCol() int { return 60 }
