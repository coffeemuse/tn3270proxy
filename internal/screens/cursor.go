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

import "github.com/racingmars/go3270"

// Cursor is a screen builder's initial input-cursor position, already adjusted
// for the 3270 attribute-byte offset. Cursor{0,0} means "home" — no input
// field to land on (e.g. an empty admin list).
type Cursor struct{ Row, Col int }

// cursorAt applies the go3270 attribute-byte rule: a field's Col is its
// attribute byte, so input begins one column to the right. This is the ONE
// place the (field.Row, field.Col+1) rule lives; every builder routes its
// primary input field through here.
func cursorAt(f go3270.Field) Cursor { return Cursor{Row: f.Row, Col: f.Col + 1} }
