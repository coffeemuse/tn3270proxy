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
	"slices"
	"sort"
)

// editorTextMax is the editable text width: each editor row spends an
// attribute byte (col 0), a 2-char prefix input (cols 1-2), and the text
// attribute (col 3), leaving cols 4-79 = 76 text columns. Lines wider than
// this render protected (display-clipped, content preserved) — full-width art
// is the import workflow's job.
const editorTextMax = 76

// applyPrefix applies one transmission's prefix commands to lines, keyed by
// GLOBAL line index. ISPF semantics: an invalid command vetoes the whole set
// (lines returned unchanged + errMsg); valid sets apply in DESCENDING index
// order so structural shifts never move a line a later command targets.
// A document can never become empty: deleting the last line leaves one "".
func applyPrefix(lines []string, cmds map[int]byte) ([]string, string) {
	for _, c := range cmds {
		if c != 'I' && c != 'D' && c != 'R' {
			return lines, "INVALID LINE COMMAND: " + string(c)
		}
	}
	idxs := make([]int, 0, len(cmds))
	for i := range cmds {
		if i >= 0 && i < len(lines) {
			idxs = append(idxs, i)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(idxs)))
	for _, i := range idxs {
		switch cmds[i] {
		case 'I':
			lines = slices.Insert(lines, i+1, "")
		case 'D':
			lines = slices.Delete(lines, i, i+1)
		case 'R':
			lines = slices.Insert(lines, i+1, lines[i])
		}
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines, ""
}
