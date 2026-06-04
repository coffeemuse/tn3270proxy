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
		{Row: 0, Col: 2, Intense: true, Content: v.Title},
		{Row: 0, Col: 60, Content: v.RowInfo},
		{Row: 2, Col: 2, Content: v.Header},
	}
	data := v.Rows
	if size := listPageSize(rows); len(data) > size {
		data = data[:size]
	}
	cur := Cursor{Row: 0, Col: 0}
	for i, r := range data {
		row := 4 + i
		cmd := go3270.Field{Row: row, Col: 2, Name: fmt.Sprintf("%s%d", fieldCmdPrefix, i), Write: true, Highlighting: go3270.Underscore}
		if i == 0 {
			cur = Cursor{Row: cmd.Row, Col: cmd.Col + 1}
		}
		screen = append(screen,
			cmd,
			go3270.Field{Row: row, Col: 4}, // stop field: 1-char command input
			go3270.Field{Row: row, Col: 7, Content: r},
		)
	}
	if len(data) == 0 {
		screen = append(screen, go3270.Field{Row: 4, Col: 7, Content: "(none)"})
	}
	screen = append(screen,
		go3270.Field{Row: legendRow(rows), Col: 2, Content: v.Legend},
		go3270.Field{Row: errorRow(rows), Col: 2, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 2, Content: v.PFHelp},
	)
	return screen, cur
}

// buildFormScreen renders v and returns the initial cursor (first input, or
// home if there are no fields).
func buildFormScreen(rows int, v FormView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: 2, Intense: true, Content: v.Title},
	}
	fields := v.Fields
	if max := formMaxFields(rows); len(fields) > max {
		fields = fields[:max]
	}
	cur := Cursor{Row: 0, Col: 0}
	for i, f := range fields {
		row := 3 + 2*i
		stopCol := 17 + f.Length
		if stopCol > 79 {
			stopCol = 79
		}
		input := go3270.Field{Row: row, Col: 16, Name: f.Name, Write: true, Hidden: f.Hidden, Content: f.Value, Highlighting: go3270.Underscore}
		if i == 0 {
			cur = Cursor{Row: input.Row, Col: input.Col + 1}
		}
		screen = append(screen,
			go3270.Field{Row: row, Col: 2, Content: f.Label},
			input,
			go3270.Field{Row: row, Col: stopCol}, // stop field
		)
	}
	screen = append(screen,
		go3270.Field{Row: errorRow(rows), Col: 2, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 2, Content: "Enter = save    PF3 = cancel"},
	)
	return screen, cur
}
