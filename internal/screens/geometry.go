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

// MenuCapacity is how many service lines fit on the menu (rows 4 .. Rows-7,
// minus one when the admin A entry needs a reserved row).
func (g Geometry) MenuCapacity(admin bool) int {
	n := g.norm().Rows - 10
	if admin {
		n--
	}
	return n
}
