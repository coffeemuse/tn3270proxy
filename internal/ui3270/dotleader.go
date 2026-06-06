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

import "strings"

// dotLeaderLabel renders label as a fixed-width field whose colon is
// right-aligned at the last column, with ISPF-style dot leaders filling the gap
// — e.g. dotLeaderLabel("MOTD File:", 23) == "MOTD File . . . . . . :". Any
// trailing spaces/colons on label are stripped and a single colon is re-applied
// at the right edge, so callers may pass labels with or without a trailing
// colon. The colon is what lines up across rows; dots fall every other position
// in the gap (the ". ." pattern), and exact dot columns vary with base length.
// If the base text is too long to leave room for a colon it is clamped to width.
// width < 1 ⇒ "".
func dotLeaderLabel(label string, width int) string {
	if width < 1 {
		return ""
	}
	base := []rune(strings.TrimRight(label, " :"))
	if len(base) >= width {
		return string(base[:width]) // no room for a colon; clamp
	}
	out := make([]rune, width)
	for i := range out {
		out[i] = ' '
	}
	copy(out, base)
	out[width-1] = ':'
	for c := len(base) + 1; c < width-1; c++ {
		if (width-1-c)%2 == 0 {
			out[c] = '.'
		}
	}
	return string(out)
}
