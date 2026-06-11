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
	"context"
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

// EditorConfig parameterizes RunEditor. Lines is the document's working copy
// seed; Save commits the full line set ("" ⇒ saved, non-"" ⇒ stay with that
// error). The driver mutates only its own copy — cancel (PF12) discards.
type EditorConfig struct {
	Title string
	Rows  int // terminal row count → page-size math
	Lines []string
	Save  func(ctx context.Context, lines []string) (errMsg string, fatal error)
}

// editorPFHelp is the editor's fixed key map (ISPF: PF3=END saves).
const editorPFHelp = "Enter=Apply    PF3=Save+End    PF7=PgUp    PF8=PgDn    PF12=Cancel"

// RunEditor drives the line editor until PF3 (save + return) or PF12 (cancel).
// Per transmission, ISPF order: text changes apply first, then prefix commands
// (I/D/R), then navigation/save. Text changes for protected (over-wide) lines
// are ignored — those lines change only by re-import. A non-nil error is a
// dead connection.
func RunEditor(ctx context.Context, r Renderer, cfg EditorConfig) error {
	lines := slices.Clone(cfg.Lines)
	if len(lines) == 0 {
		lines = []string{""}
	}
	page, errMsg := 0, ""
	for {
		var start, end int
		var rowInfo string
		page, start, end, rowInfo = pageBounds(page, len(lines), cfg.Rows)
		view := EditorView{Title: cfg.Title, RowInfo: rowInfo, ErrMsg: errMsg, PFHelp: editorPFHelp}
		for i := start; i < end; i++ {
			view.Lines = append(view.Lines, EditorLine{
				Text:      lines[i],
				Protected: len([]rune(lines[i])) > editorTextMax,
			})
		}
		act, err := r.Editor(view)
		if err != nil {
			return err
		}
		errMsg = ""

		// 1. Text changes (editable lines only; visible index + page offset).
		for vi, txt := range act.Text {
			gi := start + vi
			if gi >= 0 && gi < len(lines) && len([]rune(lines[gi])) <= editorTextMax {
				lines[gi] = txt
			}
		}
		// 2. Prefix commands (an invalid one vetoes the set and re-presents).
		if len(act.Prefix) > 0 {
			global := make(map[int]byte, len(act.Prefix))
			for vi, c := range act.Prefix {
				global[start+vi] = c
			}
			lines, errMsg = applyPrefix(lines, global)
		}
		// 3. Navigation / save.
		switch act.PF {
		case 3:
			if errMsg != "" {
				continue // bad prefix on the save press: show it, don't save
			}
			msg, ferr := cfg.Save(ctx, lines)
			if ferr != nil {
				return ferr
			}
			if msg != "" {
				errMsg = msg
				continue
			}
			return nil
		case 12:
			return nil
		case 7:
			page--
		case 8:
			if end < len(lines) {
				page++
			}
		}
	}
}
