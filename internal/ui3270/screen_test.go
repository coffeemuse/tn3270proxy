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

func TestBuildListScreenCursorPopulated(t *testing.T) {
	_, cur := buildListScreen(24, ListView{Rows: []string{"alice", "bob"}})
	// first CMD field is the flush-left attribute byte at (4,0); input one col right.
	if cur != (Cursor{Row: 4, Col: 1}) {
		t.Errorf("cursor = %+v, want {4,1}", cur)
	}
}

func TestBuildListScreenEmptyHomes(t *testing.T) {
	_, cur := buildListScreen(24, ListView{})
	if cur != (Cursor{Row: 0, Col: 0}) {
		t.Errorf("cursor = %+v, want {0,0}", cur)
	}
}

func TestBuildFormScreenCursor(t *testing.T) {
	_, cur := buildFormScreen(24, FormView{Fields: []FormField{{Name: "x", Length: 8}}})
	// first input attribute byte at (3,16); input one col right.
	if cur != (Cursor{Row: 3, Col: 17}) {
		t.Errorf("cursor = %+v, want {3,17}", cur)
	}
}

func TestBuildFormScreenReadOnlyField(t *testing.T) {
	screen, cur := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "username", Label: "User", Value: "RJLAWREN", ReadOnly: true},
		{Name: "fullname", Label: "Name", Length: 40},
	}})
	// Cursor skips the read-only field and homes to the first editable input.
	// Editable field is the 2nd row: attribute byte at (5,16), input one col right.
	if cur != (Cursor{Row: 5, Col: 17}) {
		t.Errorf("cursor = %+v, want {5,17}", cur)
	}
	// The read-only value renders as static content with no writable input field.
	var sawStatic, sawWritableUsername bool
	for _, f := range screen {
		if f.Content == "RJLAWREN" && !f.Write {
			sawStatic = true
		}
		if f.Name == "username" && f.Write {
			sawWritableUsername = true
		}
	}
	if !sawStatic {
		t.Errorf("read-only value not rendered as static content")
	}
	if sawWritableUsername {
		t.Errorf("read-only field must not be a writable input")
	}
}

// fieldByName returns the first field with the given Name (writable inputs).
func fieldByName(s go3270.Screen, name string) (go3270.Field, bool) {
	for _, f := range s {
		if f.Name == name {
			return f, true
		}
	}
	return go3270.Field{}, false
}

func TestBuildFormScreenLongLabelInputColumn(t *testing.T) {
	// "Auth Fail Window (min):" is 23 chars; the input attribute must sit at
	// col 27 and the cursor one right, so the label can't bleed into the input
	// buffer (GH #71).
	label := "Auth Fail Window (min):"
	screen, cur := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "X", Label: label, Length: 8},
	}})
	in, ok := fieldByName(screen, "X")
	if !ok {
		t.Fatal("input field X not found")
	}
	if in.Col != 27 {
		t.Errorf("input attribute col = %d, want 27", in.Col)
	}
	if cur != (Cursor{Row: 3, Col: 28}) {
		t.Errorf("cursor = %+v, want {3,28}", cur)
	}
	// Geometry guard: the label's last content column is strictly left of the
	// input attribute byte. Safe to use the full label length here because this
	// label (23) is well under labelMax, so truncRunes is a no-op.
	if last := 2 + len(label); last >= in.Col {
		t.Errorf("label ends at col %d, overlaps input attr at %d", last, in.Col)
	}
}

func TestBuildFormScreenShortLabelUnchanged(t *testing.T) {
	// A ≤12-char dot-leader label keeps the historical col 16 / cursor {3,17}
	// (byte-identical to pre-#71 — regression guard for every existing form).
	screen, cur := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "A", Label: "Group name .", Length: 32}, // 12 chars
	}})
	in, _ := fieldByName(screen, "A")
	if in.Col != 16 {
		t.Errorf("input attribute col = %d, want 16", in.Col)
	}
	if cur != (Cursor{Row: 3, Col: 17}) {
		t.Errorf("cursor = %+v, want {3,17}", cur)
	}
}

func TestBuildFormScreenInputColumnUsesLongestLabel(t *testing.T) {
	// Mixed lengths: both rows' inputs align to the longest label
	// ("Auth Delay Base (sec):", 22 → col 26).
	screen, _ := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "SHORT", Label: "MOTD File:", Length: 8},            // 10
		{Name: "LONG", Label: "Auth Delay Base (sec):", Length: 8}, // 22
	}})
	for _, name := range []string{"SHORT", "LONG"} {
		in, ok := fieldByName(screen, name)
		if !ok {
			t.Fatalf("input field %s not found", name)
		}
		if in.Col != 26 {
			t.Errorf("%s input col = %d, want 26", name, in.Col)
		}
	}
}

func TestBuildFormScreenStopFieldRebases(t *testing.T) {
	// Stop field follows the dynamic column: inputCol+1+Length = 26+1+8 = 35.
	screen, _ := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "LONG", Label: "Auth Delay Base (sec):", Length: 8}, // inputCol 26
	}})
	var sawStop bool
	for _, f := range screen {
		if f.Row == 3 && f.Col == 35 && f.Name == "" && f.Content == "" && !f.Write {
			sawStop = true
		}
	}
	if !sawStop {
		t.Errorf("stop field at (3,35) not found")
	}
}

func TestBuildFormScreenReadOnlyUsesDynamicColumn(t *testing.T) {
	// A read-only value's attribute byte aligns with the dynamic input column
	// (27 here), not a hardcoded 16, so it lines up with editable rows.
	screen, _ := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "RO", Label: "Auth Fail Window (min):", Value: "V", ReadOnly: true}, // 23 → col 27
		{Name: "ED", Label: "Auth Fail Window (min):", Length: 8},
	}})
	valCol := -1
	for _, f := range screen {
		if f.Content == "V" && !f.Write {
			valCol = f.Col
		}
	}
	if valCol != 27 {
		t.Errorf("read-only value attribute col = %d, want 27", valCol)
	}
}

func TestBuildFormScreenTruncatesPathologicalLabel(t *testing.T) {
	// A label longer than the ceiling allows is truncated so it can never reach
	// the input attribute byte (defense in depth, GH #71).
	screen, _ := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "P", Label: strings.Repeat("X", 80), Length: 8},
	}})
	in, _ := fieldByName(screen, "P")
	if in.Col != 63 { // clamped to the ceiling
		t.Errorf("input col = %d, want ceiling 63", in.Col)
	}
	labelLen := -1
	for _, f := range screen {
		if f.Row == 3 && f.Col == 2 { // the label attribute byte
			labelLen = len([]rune(f.Content))
		}
	}
	if labelLen != 59 { // formLabelMax(63)
		t.Errorf("label truncated to %d runes, want 59", labelLen)
	}
	if last := 2 + labelLen; last >= in.Col {
		t.Errorf("truncated label reaches col %d, overlaps input attr at %d", last, in.Col)
	}
}

func TestBuildFormScreenDotLeader(t *testing.T) {
	// With DotLeader on, labels are dot-leader padded to the label-field width so
	// colons align; the dynamic input column is unchanged (still driven by the
	// longest RAW label, 23 → col 27).
	fields := []FormField{
		{Name: "A", Label: "MOTD File:", Length: 8},
		{Name: "B", Label: "Auth Fail Window (min):", Length: 8},
	}
	screen, _ := buildFormScreen(24, FormView{DotLeader: true, Fields: fields})
	want := map[int]string{
		3: "MOTD File . . . . . . :",
		5: "Auth Fail Window (min):",
	}
	seen := 0
	for _, f := range screen {
		if w, ok := want[f.Row]; ok && f.Col == 2 {
			seen++
			if f.Content != w {
				t.Errorf("row %d label = %q, want %q", f.Row, f.Content, w)
			}
		}
	}
	if seen != len(want) {
		t.Errorf("found %d of %d expected label rows", seen, len(want))
	}
	in, ok := fieldByName(screen, "A")
	if !ok {
		t.Fatal("input field A not found")
	}
	if in.Col != 27 {
		t.Errorf("input col = %d, want 27 (unchanged by dot-leader)", in.Col)
	}
}

func TestBuildFormScreenDotLeaderOffUnchanged(t *testing.T) {
	// Default (DotLeader off): label rendered raw (no dot fill).
	screen, _ := buildFormScreen(24, FormView{Fields: []FormField{
		{Name: "A", Label: "MOTD File:", Length: 8},
	}})
	found := false
	for _, f := range screen {
		if f.Row == 3 && f.Col == 2 {
			found = true
			if f.Content != "MOTD File:" {
				t.Errorf("row 3 label = %q, want raw %q", f.Content, "MOTD File:")
			}
		}
	}
	if !found {
		t.Error("label field at row 3 col 2 not found in screen")
	}
}

func TestBuildFormScreenCompactSingleSpaced(t *testing.T) {
	screen, cur := buildFormScreen(24, FormView{Compact: true, Fields: []FormField{
		{Name: "a", Label: "A", Length: 8},
		{Name: "b", Label: "B", Length: 8},
	}})
	a, _ := fieldByName(screen, "a")
	b, _ := fieldByName(screen, "b")
	if a.Row != 2 || b.Row != 3 {
		t.Errorf("rows = %d,%d, want 2,3 (single-spaced from row 2)", a.Row, b.Row)
	}
	if cur != (Cursor{Row: 2, Col: a.Col + 1}) {
		t.Errorf("cursor = %+v, want first writable input", cur)
	}
}

func TestBuildFormScreenCompactMessageRowAtBottom(t *testing.T) {
	screen, _ := buildFormScreen(24, FormView{Compact: true, Fields: []FormField{
		{Name: "a", Label: "A", Length: 8},
	}})
	msg, ok := fieldByName(screen, fieldError)
	if !ok {
		t.Fatal("error field not found")
	}
	if msg.Row != 22 {
		t.Errorf("message row = %d, want 22", msg.Row)
	}
}

func TestBuildListScreenPalette(t *testing.T) {
	screen, cur := buildListScreen(24, ListView{
		Title: "USER ADMINISTRATION", RowInfo: "ROW 1 TO 2 OF 2",
		Header: "Userid    Name", Rows: []string{"ALICE  Alice"},
		Legend: "S=Select", ErrMsg: "boom", PFHelp: "PF3=Back",
	})
	title, ok := fieldAt(screen, 0, centerCol(len("USER ADMINISTRATION")))
	if !ok || title.Content != "USER ADMINISTRATION" || title.Color != go3270.White || !title.Intense {
		t.Errorf("title = %+v ok=%v, want centered white intense", title, ok)
	}
	hdr, ok := fieldAt(screen, bodyTopRow(), 0)
	if !ok || hdr.Color != go3270.Blue || hdr.Content != "Userid    Name" {
		t.Errorf("header = %+v ok=%v, want row 3 col 0 blue", hdr, ok)
	}
	msg, ok := fieldByName(screen, fieldError)
	if !ok || msg.Row != 2 {
		t.Errorf("message row = %d ok=%v, want 2", msg.Row, ok)
	}
	if cur != (Cursor{Row: 4, Col: 1}) {
		t.Errorf("cursor = %+v, want {4,1} (flush-left)", cur)
	}
}

func TestBuildFormScreenPalette(t *testing.T) {
	screen, _ := buildFormScreen(24, FormView{
		Title:  "EDIT USER",
		Fields: []FormField{{Name: "x", Label: "Name", Length: 8}},
		ErrMsg: "boom",
	})
	title, ok := fieldAt(screen, 0, centerCol(len("EDIT USER")))
	if !ok || title.Content != "EDIT USER" || title.Color != go3270.White || !title.Intense {
		t.Errorf("title = %+v ok=%v, want centered white intense", title, ok)
	}
	label, ok := fieldAt(screen, 3, labelAttrCol)
	if !ok || label.Color != go3270.Turquoise {
		t.Errorf("label = %+v ok=%v, want turquoise", label, ok)
	}
	in, _ := fieldByName(screen, "x")
	if in.Color != go3270.Green {
		t.Errorf("input color = %v, want green", in.Color)
	}
	msg, ok := fieldByName(screen, fieldError)
	if !ok || msg.Row != 2 || msg.Color != go3270.Red || !msg.Intense {
		t.Errorf("message = %+v ok=%v, want row 2 red intense", msg, ok)
	}
}

func TestBuildFormScreenCompactFirstSectionNoGutter(t *testing.T) {
	screen, _ := buildFormScreen(24, FormView{Compact: true, Fields: []FormField{
		{Name: "a", Label: "A", Length: 8, Section: "Identity"},
	}})
	bannerRow := -1
	for _, f := range screen {
		if strings.HasPrefix(f.Content, "--- Identity") {
			bannerRow = f.Row
		}
	}
	if bannerRow != 2 {
		t.Errorf("banner row = %d, want 2 (no gutter before the first section)", bannerRow)
	}
	a, _ := fieldByName(screen, "a")
	if a.Row != 3 {
		t.Errorf("field row = %d, want 3", a.Row)
	}
}

func TestBuildFormScreenCompactSectionGutter(t *testing.T) {
	screen, _ := buildFormScreen(24, FormView{Compact: true, Fields: []FormField{
		{Name: "a", Label: "A", Length: 8},                  // row 2
		{Name: "b", Label: "B", Length: 8, Section: "Sect"}, // gutter 3, banner 4, field 5
	}})
	a, _ := fieldByName(screen, "a")
	b, _ := fieldByName(screen, "b")
	if a.Row != 2 || b.Row != 5 {
		t.Errorf("rows = %d,%d, want 2,5", a.Row, b.Row)
	}
	bannerRow := -1
	for _, f := range screen {
		if strings.HasPrefix(f.Content, "--- Sect") {
			bannerRow = f.Row
		}
	}
	if bannerRow != 4 {
		t.Errorf("banner row = %d, want 4 (gutter at 3)", bannerRow)
	}
}
