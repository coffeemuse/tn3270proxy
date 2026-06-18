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

const (
	fieldPfxPrefix = "pfx" // per-row 2-char prefix command inputs: pfx0, pfx1, …
	fieldTxtPrefix = "txt" // per-row text inputs: txt0, txt1, …
)

// editorRuler is the ISPF-style column ruler over the 76-col text area
// (exactly editorTextMax chars).
const editorRuler = "----+----1----+----2----+----3----+----4----+----5----+----6----+----7----+-"

// buildEditorScreen renders one editor page. Row layout per line: prefix
// attribute col 0, prefix input cols 1-2, text attribute col 3, text cols
// 4-79. Editable text is written WITHOUT padding so the field's trailing
// positions stay NUL — that is what makes native 3270 insert mode work; do not
// "fix" this by padding with spaces (locks the keyboard on Insert).
func buildEditorScreen(rows int, v EditorView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: centerCol(len(v.Title)), Color: go3270.White, Intense: true, Content: v.Title},
		{Row: 0, Col: 60, Content: v.RowInfo},
		// Attribute byte at col 3 puts the ruler's first char at col 4,
		// aligned with the text columns below it.
		{Row: bodyTopRow(), Col: 3, Color: go3270.Blue, Content: editorRuler},
	}
	cur := Cursor{Row: 0, Col: 0}
	row := 4
	for i, ln := range v.Lines {
		row = 4 + i
		pfx := go3270.Field{Row: row, Col: 0, Name: fmt.Sprintf("%s%d", fieldPfxPrefix, i),
			Write: true, Color: go3270.Green, Highlighting: go3270.Underscore}
		if i == 0 {
			cur = Cursor{Row: pfx.Row, Col: pfx.Col + 1}
		}
		screen = append(screen, pfx)
		if ln.Protected {
			screen = append(screen, go3270.Field{Row: row, Col: 3, Color: go3270.Yellow,
				Content: truncRunes(ln.Text, editorTextMax)})
		} else {
			// KeepSpaces: go3270 otherwise TrimSpaces the returned value,
			// destroying leading whitespace (indents, ASCII-art positioning).
			// Trailing spaces/NULs are trimmed by editorAction instead.
			screen = append(screen, go3270.Field{Row: row, Col: 3,
				Name: fmt.Sprintf("%s%d", fieldTxtPrefix, i), Write: true,
				KeepSpaces: true, Color: go3270.Green, Content: ln.Text})
		}
	}
	// Stop field: the last text field would otherwise run through the blank
	// rows below it into the legend (a 3270 field ends only at the next
	// attribute byte).
	screen = append(screen, go3270.Field{Row: row + 1, Col: 0})
	screen = append(screen,
		go3270.Field{Row: legendRow(rows), Col: 2, Color: go3270.Turquoise,
			Content: "Prefix: I=Insert  D=Delete  R=Repeat    Yellow lines: re-import to change"},
		go3270.Field{Row: messageRow(), Col: 2, Name: fieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: helpRow(rows), Col: 0, Color: go3270.Blue, Content: v.PFHelp},
	)
	return screen, cur
}

// editorAction maps a HandleScreen response to an EditorAction. Text trims
// trailing spaces/NULs only — leading whitespace is significant (art).
// A field absent from resp.Values is treated as unchanged (defensive).
func editorAction(resp go3270.Response, nLines int) EditorAction {
	act := EditorAction{Prefix: map[int]byte{}, Text: map[int]string{}}
	switch resp.AID {
	case go3270.AIDPF3:
		act.PF = 3
	case go3270.AIDPF7:
		act.PF = 7
	case go3270.AIDPF8:
		act.PF = 8
	case go3270.AIDPF12:
		act.PF = 12
	}
	for i := 0; i < nLines; i++ {
		if v, ok := resp.Values[fmt.Sprintf("%s%d", fieldPfxPrefix, i)]; ok {
			if c := strings.ToUpper(strings.TrimSpace(v)); c != "" {
				act.Prefix[i] = c[0]
			}
		}
		if v, ok := resp.Values[fmt.Sprintf("%s%d", fieldTxtPrefix, i)]; ok {
			act.Text[i] = strings.TrimRight(v, " \x00")
		}
	}
	return act
}
