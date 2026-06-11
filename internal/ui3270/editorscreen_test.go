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
	"strings"
	"testing"

	"github.com/racingmars/go3270"
)

func findFieldByName(s go3270.Screen, name string) (go3270.Field, bool) {
	for _, f := range s {
		if f.Name == name {
			return f, true
		}
	}
	return go3270.Field{}, false
}

func TestBuildEditorScreenEditableLine(t *testing.T) {
	v := EditorView{
		Title:  "EDIT MOTD",
		Lines:  []EditorLine{{Text: "HELLO"}, {Text: "WORLD"}},
		PFHelp: "PF3=Save  PF7/PF8=Page  PF12=Cancel",
	}
	screen, cur := buildEditorScreen(24, v)

	pfx, ok := findFieldByName(screen, "pfx0")
	if !ok || !pfx.Write || pfx.Col != 0 {
		t.Fatalf("pfx0: %+v ok=%v (want writable at col 0)", pfx, ok)
	}
	txt, ok := findFieldByName(screen, "txt0")
	if !ok || !txt.Write || txt.Col != 3 || txt.Content != "HELLO" {
		t.Fatalf("txt0: %+v ok=%v (want writable at col 3, content HELLO)", txt, ok)
	}
	// Content must NOT be space-padded: trailing field positions stay NUL so
	// native 3270 insert mode works (CLAUDE.md gotcha).
	if strings.HasSuffix(txt.Content, " ") || len(txt.Content) != len("HELLO") {
		t.Errorf("txt0 content padded: %q", txt.Content)
	}
	// go3270 TrimSpaces every field value unless KeepSpaces is set — without
	// it, leading whitespace (paragraph indents, ASCII-art positioning) is
	// destroyed before editorAction ever sees the value. Found in live QA;
	// synthetic-Response tests cannot catch it.
	if !txt.KeepSpaces {
		t.Error("txt0 missing KeepSpaces: go3270 would strip leading whitespace")
	}
	// Cursor on the first prefix input (attribute byte + 1).
	if cur != (Cursor{Row: pfx.Row, Col: pfx.Col + 1}) {
		t.Errorf("cursor %+v, want {%d %d}", cur, pfx.Row, pfx.Col+1)
	}
	// Stop field terminates the LAST text field: an unnamed protected field at
	// col 0 on the row after the last line.
	last, _ := findFieldByName(screen, "txt1")
	foundStop := false
	for _, f := range screen {
		if f.Name == "" && f.Col == 0 && f.Row == last.Row+1 && !f.Write {
			foundStop = true
		}
	}
	if !foundStop {
		t.Error("missing stop field after last editor line")
	}
}

func TestBuildEditorScreenProtectedWideLine(t *testing.T) {
	wide := strings.Repeat("X", 80)
	v := EditorView{Title: "EDIT BRANDING", Lines: []EditorLine{{Text: wide, Protected: true}}}
	screen, _ := buildEditorScreen(24, v)
	if _, ok := findFieldByName(screen, "txt0"); ok {
		t.Error("protected line must not have a writable text field")
	}
	found := false
	for _, f := range screen {
		if f.Col == 3 && !f.Write && f.Content == wide[:editorTextMax] && f.Color == go3270.Yellow {
			found = true
		}
	}
	if !found {
		t.Error("protected line not rendered as yellow clipped static text")
	}
	if pfx, ok := findFieldByName(screen, "pfx0"); !ok || !pfx.Write {
		t.Error("protected line must keep its prefix input")
	}
}

func TestEditorAction(t *testing.T) {
	resp := go3270.Response{
		AID: go3270.AIDEnter,
		Values: map[string]string{
			"pfx0": " d ",
			"pfx1": "",
			"txt0": "  indented art   ",
			"txt1": "unchanged",
		},
	}
	act := editorAction(resp, 2)
	if act.PF != 0 {
		t.Errorf("PF = %d, want 0 (Enter)", act.PF)
	}
	if act.Prefix[0] != 'D' || len(act.Prefix) != 1 {
		t.Errorf("Prefix = %v, want {0:'D'}", act.Prefix)
	}
	// Leading spaces preserved (art!), trailing spaces/NULs stripped.
	if act.Text[0] != "  indented art" {
		t.Errorf("Text[0] = %q", act.Text[0])
	}
	for _, aid := range []struct {
		aid go3270.AID
		pf  int
	}{{go3270.AIDPF3, 3}, {go3270.AIDPF7, 7}, {go3270.AIDPF8, 8}, {go3270.AIDPF12, 12}} {
		if got := editorAction(go3270.Response{AID: aid.aid}, 0); got.PF != aid.pf {
			t.Errorf("AID %v → PF %d, want %d", aid.aid, got.PF, aid.pf)
		}
	}
}
