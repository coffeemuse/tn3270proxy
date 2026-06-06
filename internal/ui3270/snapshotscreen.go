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
	"strings"

	"github.com/racingmars/go3270"
)

// Snapshot row column geometry (0-based; attribute byte at the named column,
// content one column right). The Mid/Right attribute bytes double as the
// column separators, so the visible budget is Left 20 (8-27), Mid 12 (29-40),
// Right 38 (42-79).
const (
	snapCmdAttr   = 2  // line-command field attr; input at col 3
	snapCmdStop   = 4  // stop field: 1-char command input
	snapLeftAttr  = 7  // Left segment attr; content cols 8-27
	snapMidAttr   = 28 // Mid (event) attr; content cols 29-40
	snapRightAttr = 41 // Right (detail) attr; content cols 42-79
)

// snapSegments appends the three display fields for one snapshot row at the
// given terminal row. The Mid field carries midColor (DefaultColor ⇒ plain).
func snapSegments(row int, r SnapshotRow) go3270.Screen {
	return go3270.Screen{
		{Row: row, Col: snapLeftAttr, Content: r.Left},
		{Row: row, Col: snapMidAttr, Content: r.Mid, Color: r.MidColor},
		{Row: row, Col: snapRightAttr, Content: r.Right},
	}
}

// buildSnapshotScreen renders a read-only paged list: title + row indicator on
// row 0, the as-of stamp on row 2, column headings on row 3, data from row 4,
// and the bottom-anchored legend/error/help rows. Cursor homes to the first
// command field, or {0,0} when the page is empty.
func buildSnapshotScreen(rows int, v SnapshotView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: 2, Intense: true, Content: v.Title},
		{Row: 0, Col: 60, Content: v.RowInfo},
		{Row: 2, Col: snapLeftAttr, Content: v.AsOf},
	}
	screen = append(screen, snapSegments(3, v.Head)...)

	data := v.Rows
	if size := listPageSize(rows); len(data) > size {
		data = data[:size]
	}
	cur := Cursor{Row: 0, Col: 0}
	for i, r := range data {
		row := 4 + i
		cmd := go3270.Field{Row: row, Col: snapCmdAttr, Name: fmt.Sprintf("%s%d", fieldCmdPrefix, i), Write: true, Highlighting: go3270.Underscore}
		if i == 0 {
			cur = Cursor{Row: cmd.Row, Col: cmd.Col + 1}
		}
		screen = append(screen, cmd, go3270.Field{Row: row, Col: snapCmdStop})
		screen = append(screen, snapSegments(row, r)...)
	}
	if len(data) == 0 {
		screen = append(screen, go3270.Field{Row: 4, Col: snapLeftAttr, Content: v.Empty})
	}
	screen = append(screen,
		go3270.Field{Row: legendRow(rows), Col: 2, Content: v.Legend},
		go3270.Field{Row: errorRow(rows), Col: 2, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 2, Content: v.PFHelp},
	)
	return screen, cur
}

// detailValueCol is the attribute-byte column of the value fields on the detail
// screen (content one column right). Matches the form's historical input column.
const detailValueCol = 16

// detailBodyWidth is the wrap width for the free-text body (content cols 3-79).
const detailBodyWidth = 76

// wrapText greedily wraps s to lines of at most width runes, breaking on spaces.
// A token longer than width is hard-split. Returns nil for empty input.
func wrapText(s string, width int) []string {
	if width < 1 || s == "" {
		if s == "" {
			return nil
		}
		width = 1
	}
	var lines []string
	cur := ""
	flush := func() {
		if cur != "" {
			lines = append(lines, cur)
			cur = ""
		}
	}
	for _, word := range strings.Fields(s) {
		for len([]rune(word)) > width { // hard-split an over-long token
			flush()
			r := []rune(word)
			lines = append(lines, string(r[:width]))
			word = string(r[width:])
		}
		switch {
		case cur == "":
			cur = word
		case len([]rune(cur))+1+len([]rune(word)) <= width:
			cur += " " + word
		default:
			flush()
			cur = word
		}
	}
	flush()
	return lines
}

// buildDetailScreen renders a read-only record: title on row 0, label/value
// fields from row 2, then BodyLabel and the wrapped Body. Cursor homes ({0,0}).
func buildDetailScreen(rows int, v DetailView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: 2, Intense: true, Content: v.Title},
	}
	row := 2
	for _, f := range v.Fields {
		screen = append(screen,
			go3270.Field{Row: row, Col: 2, Content: f.Label},
			go3270.Field{Row: row, Col: detailValueCol, Content: f.Value, Color: f.Color},
		)
		row++
	}
	row++ // blank separator
	if v.BodyLabel != "" {
		screen = append(screen, go3270.Field{Row: row, Col: 2, Content: v.BodyLabel})
		row++
	}
	for _, line := range wrapText(v.Body, detailBodyWidth) {
		if row >= errorRow(rows) { // never overrun the bottom chrome
			break
		}
		screen = append(screen, go3270.Field{Row: row, Col: 2, Content: line})
		row++
	}
	screen = append(screen, go3270.Field{Row: helpRow(rows), Col: 2, Content: v.PFHelp})
	return screen, Cursor{Row: 0, Col: 0}
}
