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

// ErrorRow is the red error/message line (21 on MOD 2).
func (g Geometry) ErrorRow() int { return g.norm().Rows - 3 }

// LegendRow is the admin-list line-command legend (20 on MOD 2).
func (g Geometry) LegendRow() int { return g.norm().Rows - 4 }

// InputRow is the "===>" command/selection input line (19 on MOD 2).
func (g Geometry) InputRow() int { return g.norm().Rows - 5 }

// ListPageSize is how many data rows fit on an admin list screen
// (rows 4 .. Rows-7; 14 on MOD 2).
func (g Geometry) ListPageSize() int { return g.norm().Rows - 10 }

// FormMaxFields is how many labeled inputs fit on an admin form
// (rows 3, 5, 7, …, two apart, ending at Rows-5 or Rows-6 by parity;
// 9 on MOD 2).
func (g Geometry) FormMaxFields() int { return (g.norm().Rows-8)/2 + 1 }

// MenuCapacity is how many service lines fit on the menu: body rows
// BodyTopRow+1 .. BodyBottomRow (row BodyTopRow is the instruction line), minus
// one for the always-present "0 User Settings" row and one more for the admin
// "A" row.
func (g Geometry) MenuCapacity(admin bool) int {
	n := g.BodyBottomRow() - (g.BodyTopRow() + 1) + 1 - 1 // body rows minus the "0" meta row
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
// panel). It is fixed at 57: the service grid (number col 0, name col 4,
// description col 13 hard-cut to 40) ends near col 53, and the block's 10-char
// labels + 7-rune values fit within cols 57-79. Content is always within cols
// 0-79 (rows-only adaptation), so this does not widen on taller models.
func (g Geometry) StatusBlockCol() int { return 57 }
