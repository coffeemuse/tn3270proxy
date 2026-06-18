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
		{Row: bodyTopRow(), Col: 0, Color: go3270.Blue, Content: v.Header},
	}
	data := v.Rows
	if size := listPageSize(rows); len(data) > size {
		data = data[:size]
	}
	cur := Cursor{Row: 0, Col: 0}
	for i, r := range data {
		row := 4 + i
		// CMD line-command field is flush-left (ISPF convention): attribute byte
		// at col 0, 1-char writable input at col 1, stop field at col 2, then the
		// row content at col 4. Headers prefix "CMD " (4 cols) to match.
		cmd := go3270.Field{Row: row, Col: 0, Name: fmt.Sprintf("%s%d", fieldCmdPrefix, i), Write: true, Color: go3270.Green, Highlighting: go3270.Underscore}
		if i == 0 {
			cur = Cursor{Row: cmd.Row, Col: cmd.Col + 1}
		}
		screen = append(screen,
			cmd,
			go3270.Field{Row: row, Col: 2}, // stop field: 1-char command input
			go3270.Field{Row: row, Col: 4, Color: go3270.Green, Content: r},
		)
	}
	if len(data) == 0 {
		screen = append(screen, go3270.Field{Row: 4, Col: 4, Content: "(none)"})
	}
	screen = append(screen,
		go3270.Field{Row: legendRow(rows), Col: 0, Color: go3270.Turquoise, Content: v.Legend},
		go3270.Field{Row: messageRow(), Col: 0, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 0, Color: go3270.Blue, Content: v.PFHelp},
	)
	return screen, cur
}

// buildFormScreen renders v and returns the initial cursor (first input, or
// home if there are no fields).
func buildFormScreen(rows int, v FormView) (go3270.Screen, Cursor) {
	if v.Compact {
		return buildCompactFormScreen(rows, v)
	}
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
		placeFormField(&screen, &cur, f, row, labelAttrCol, inputCol, labelMax, v.DotLeader)
	}
	screen = append(screen,
		go3270.Field{Row: messageRow(), Col: 2, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 0, Color: go3270.Blue, Content: "Enter=Save    PF3=Cancel"},
	)
	return screen, cur
}

// placeFormField appends the go3270 fields for one labeled input at the given
// row: a label at labelCol, then either a static value (ReadOnly) or a writable
// input at inputCol with its stop field, plus an optional Suffix. It updates cur
// to the first writable input encountered (the first call that sets it wins).
func placeFormField(screen *go3270.Screen, cur *Cursor, f FormField, row, labelCol, inputCol, labelMax int, dotLeader bool) {
	label := truncRunes(f.Label, labelMax)
	if dotLeader {
		label = dotLeaderLabel(f.Label, labelMax)
	}
	if f.ReadOnly {
		*screen = append(*screen,
			go3270.Field{Row: row, Col: labelCol, Color: go3270.Turquoise, Content: label},
			go3270.Field{Row: row, Col: inputCol, Color: go3270.Green, Content: f.Value},
		)
		return
	}
	stopCol := min(inputCol+1+f.Length, 79)
	input := go3270.Field{Row: row, Col: inputCol, Name: f.Name, Write: true, Hidden: f.Hidden, Content: f.Value, Color: go3270.Green, Highlighting: go3270.Underscore}
	if *cur == (Cursor{Row: 0, Col: 0}) {
		*cur = Cursor{Row: input.Row, Col: input.Col + 1}
	}
	*screen = append(*screen,
		go3270.Field{Row: row, Col: labelCol, Color: go3270.Turquoise, Content: label},
		input,
		go3270.Field{Row: row, Col: stopCol}, // stop field
	)
	if f.Suffix != "" {
		*screen = append(*screen, go3270.Field{Row: row, Col: stopCol + 1, Color: go3270.Turquoise, Content: f.Suffix})
	}
}

// buildCompactFormScreen renders v in the sectioned, single-spaced layout. The
// body starts at row 2; Section banners (with a leading gutter row except before
// the first section) group fields; SameRow pairs a field onto the previous row's
// second column; Suffix adds trailing hint text; the message line sits at the
// bottom. Cursor lands on the first writable input.
func buildCompactFormScreen(rows int, v FormView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: centerCol(len(v.Title)), Color: go3270.White, Intense: true, Content: v.Title},
	}
	// Input column derives from the longest PRIMARY (non-SameRow) label.
	maxLabel := 0
	for _, f := range v.Fields {
		if f.SameRow {
			continue
		}
		if n := len([]rune(f.Label)); n > maxLabel {
			maxLabel = n
		}
	}
	inputCol := formInputCol(maxLabel)
	labelMax := formLabelMax(inputCol)

	cur := Cursor{Row: 0, Col: 0}
	row := compactBodyTopRow
	lastRow := row
	limit := compactMessageRow(rows) - 1
	first := true
	for _, f := range v.Fields {
		if f.SameRow {
			if first {
				continue // no preceding primary field to share a row with
			}
			placeFormField(&screen, &cur, f, lastRow, sameRowLabelCol, sameRowInputCol, sameRowLabelMax, v.DotLeader)
			continue
		}
		if f.Section != "" {
			if !first {
				row++ // blank gutter row before the section banner
			}
			// Out of vertical room for the banner: stop before the message line.
			if row > limit {
				break
			}
			screen = append(screen, go3270.Field{Row: row, Col: labelAttrCol, Color: go3270.Blue, Content: sectionBanner(f.Section)})
			row++
		}
		// Out of vertical room for the field: stop before overlapping the message
		// line. We break (not continue), so leaving `first` unset here is harmless.
		if row > limit {
			break
		}
		placeFormField(&screen, &cur, f, row, labelAttrCol, inputCol, labelMax, v.DotLeader)
		lastRow = row
		row++
		first = false
	}
	screen = append(screen,
		go3270.Field{Row: compactMessageRow(rows), Col: 2, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 0, Color: go3270.Blue, Content: "Enter=Save    PF3=Cancel"},
	)
	return screen, cur
}
