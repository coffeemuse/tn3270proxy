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
	"testing"

	"github.com/racingmars/go3270"
)

func TestCursorAtAppliesAttributeOffset(t *testing.T) {
	// A field's Col is its attribute byte; input begins one column right.
	f := go3270.Field{Row: 5, Col: 16}
	if got, want := cursorAt(f), (Cursor{Row: 5, Col: 17}); got != want {
		t.Errorf("cursorAt(%+v) = %+v, want %+v", f, got, want)
	}
}
