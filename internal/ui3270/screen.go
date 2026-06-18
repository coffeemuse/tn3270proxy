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
	"fmt"

	"github.com/racingmars/go3270"
)

const (
	fieldError     = "errormsg" // matches the historical screens.FieldError value
	fieldCmdPrefix = "cmd"      // per-row line-command inputs: cmd0, cmd1, …
)

// buildListScreen renders v at the given terminal size and returns the initial
// cursor (first CMD field, or home for an empty list).
func buildListScreen(rows int, v ListView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: centerCol(len(v.Title)), Color: go3270.White, Intense: true, Content: v.Title},
		{Row: 0, Col: 60, Content: v.RowInfo},
		{Row: bodyTopRow(), Col: 2, Color: go3270.Blue, Content: v.Header},
	}
	data := v.Rows
	if size := listPageSize(rows); len(data) > size {
		data = data[:size]
	}
	cur := Cursor{Row: 0, Col: 0}
	for i, r := range data {
		row := 4 + i
		cmd := go3270.Field{Row: row, Col: 2, Name: fmt.Sprintf("%s%d", fieldCmdPrefix, i), Write: true, Color: go3270.Green, Highlighting: go3270.Underscore}
		if i == 0 {
			cur = Cursor{Row: cmd.Row, Col: cmd.Col + 1}
		}
		screen = append(screen,
			cmd,
			go3270.Field{Row: row, Col: 4}, // stop field: 1-char command input
			go3270.Field{Row: row, Col: 7, Color: go3270.Green, Content: r},
		)
	}
	if len(data) == 0 {
		screen = append(screen, go3270.Field{Row: 4, Col: 7, Content: "(none)"})
	}
	screen = append(screen,
		go3270.Field{Row: legendRow(rows), Col: 2, Color: go3270.Turquoise, Content: v.Legend},
		go3270.Field{Row: messageRow(), Col: 2, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 0, Color: go3270.Blue, Content: v.PFHelp},
	)
	return screen, cur
}

// buildFormScreen renders v and returns the initial cursor (first input, or
// home if there are no fields).
func buildFormScreen(rows int, v FormView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: centerCol(len(v.Title)), Color: go3270.White, Intense: true, Content: v.Title},
	}
	fields := v.Fields
	if max := formMaxFields(rows); len(fields) > max {
		fields = fields[:max]
	}
	// Place the input column past the longest label so label text can never
	// overrun the input field's buffer (GH #71). Forms whose labels are ≤12
	// chars compute the historical col 16 and render byte-identically.
	maxLabel := 0
	for _, f := range fields {
		if n := len([]rune(f.Label)); n > maxLabel {
			maxLabel = n
		}
	}
	inputCol := formInputCol(maxLabel)
	labelMax := formLabelMax(inputCol)
	cur := Cursor{Row: 0, Col: 0}
	for i, f := range fields {
		row := 3 + 2*i
		label := truncRunes(f.Label, labelMax)
		if v.DotLeader {
			label = dotLeaderLabel(f.Label, labelMax)
		}
		if f.ReadOnly {
			// Display-only: label + static value, no writable input, never the
			// cursor target.
			screen = append(screen,
				go3270.Field{Row: row, Col: labelAttrCol, Color: go3270.Turquoise, Content: label},
				go3270.Field{Row: row, Col: inputCol, Color: go3270.Green, Content: f.Value},
			)
			continue
		}
		stopCol := min(inputCol+1+f.Length, 79)
		input := go3270.Field{Row: row, Col: inputCol, Name: f.Name, Write: true, Hidden: f.Hidden, Content: f.Value, Color: go3270.Green, Highlighting: go3270.Underscore}
		if cur == (Cursor{Row: 0, Col: 0}) {
			cur = Cursor{Row: input.Row, Col: input.Col + 1}
		}
		screen = append(screen,
			go3270.Field{Row: row, Col: labelAttrCol, Color: go3270.Turquoise, Content: label},
			input,
			go3270.Field{Row: row, Col: stopCol}, // stop field
		)
	}
	screen = append(screen,
		go3270.Field{Row: messageRow(), Col: 2, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 0, Color: go3270.Blue, Content: "Enter=Save    PF3=Cancel"},
	)
	return screen, cur
}
