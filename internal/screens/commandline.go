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

// commandLine builds the ISPF command line at CommandRow: a turquoise prompt
// ("Option ===>" on menus, "Command ===>" on lists) and the named green input
// field placed just past the prompt. It returns the prompt, input, and stop
// fields separately so callers can tune the input (e.g. NumericOnly) and compute
// the cursor with cursorAt(input). See docs/ispf-style-guide.md §2.
func commandLine(geom Geometry, prompt, fieldName string) (promptF, input, stop go3270.Field) {
	const promptAttrCol = 2
	// Prompt content runs cols 3..(2+len); the next field's attribute byte sits
	// one past it, giving a one-column gap.
	inputCol := promptAttrCol + 1 + len([]rune(prompt))
	promptF = go3270.Field{Row: geom.CommandRow(), Col: promptAttrCol, Color: go3270.Turquoise, Content: prompt}
	input = go3270.Field{Row: geom.CommandRow(), Col: inputCol, Name: fieldName, Write: true, Color: go3270.Green, Highlighting: go3270.Underscore}
	stop = go3270.Field{Row: geom.CommandRow(), Col: min(inputCol+9, 79)} // ~8-col input window
	return promptF, input, stop
}
