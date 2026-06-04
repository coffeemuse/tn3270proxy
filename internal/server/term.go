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

package server

import (
	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/racingmars/go3270"
)

// Term describes the negotiated client terminal. The real presenter's
// Negotiate fills dev so screens render at the terminal's alternate size with
// its detected codepage; test fakes construct Term directly and leave dev nil
// (go3270 cannot be faked: DevInfo has a private method), which renders plain
// 24×80 — exactly the fallback behavior.
type Term struct {
	Type       string // negotiated terminal type, e.g. "IBM-3278-4-E"
	Rows, Cols int    // alternate screen dimensions

	dev go3270.DevInfo // nil ⇒ HandleScreenAlt behaves like HandleScreen (24×80)
}

// normalizeTerm applies the fallback rule: an unknown or sub-MOD 2 alternate
// size renders as plain 24×80 (dev dropped so go3270 never writes to an
// alternate buffer smaller than the layout).
func normalizeTerm(t Term) Term {
	if t.Rows < 24 || t.Cols < 80 {
		t.Rows, t.Cols, t.dev = 24, 80, nil
	}
	return t
}

// Geometry converts to the screens-layer value. screens.Geometry normalizes
// again on its own — belt and braces for fakes that skip normalizeTerm.
func (t Term) Geometry() screens.Geometry {
	return screens.Geometry{Rows: t.Rows, Cols: t.Cols}
}

// codepage is the detected client codepage; nil selects go3270's default
// (code page 1047, the pre-existing behavior).
func (t Term) codepage() go3270.Codepage {
	if t.dev == nil {
		return nil
	}
	return t.dev.Codepage()
}
