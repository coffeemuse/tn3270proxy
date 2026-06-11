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
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestRunEditorTypeAndSave(t *testing.T) {
	f := &fakeRenderer{editorActs: []EditorAction{
		{Text: map[int]string{0: "EDITED"}, PF: 3}, // type over line 0, PF3=save
	}}
	var saved []string
	err := RunEditor(context.Background(), f, EditorConfig{
		Title: "EDIT MOTD", Rows: 24, Lines: []string{"HELLO", "WORLD"},
		Save: func(_ context.Context, lines []string) (string, error) {
			saved = slices.Clone(lines)
			return "", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(saved, []string{"EDITED", "WORLD"}) {
		t.Errorf("saved %q", saved)
	}
}

func TestRunEditorPrefixThenCancelDiscards(t *testing.T) {
	f := &fakeRenderer{editorActs: []EditorAction{
		{Prefix: map[int]byte{0: 'D'}}, // Enter: delete line 0
		{PF: 12},                       // PF12: cancel
	}}
	saveCalled := false
	err := RunEditor(context.Background(), f, EditorConfig{
		Title: "EDIT MOTD", Rows: 24, Lines: []string{"A", "B"},
		Save: func(_ context.Context, _ []string) (string, error) {
			saveCalled = true
			return "", nil
		},
	})
	if err != nil || saveCalled {
		t.Errorf("err=%v saveCalled=%v; cancel must not save", err, saveCalled)
	}
	// Second paint reflects the delete (working copy mutated before cancel).
	if got := f.editorViews[1].Lines; len(got) != 1 || got[0].Text != "B" {
		t.Errorf("second paint lines: %+v", got)
	}
}

func TestRunEditorInvalidPrefixVetoesPF3Save(t *testing.T) {
	f := &fakeRenderer{editorActs: []EditorAction{
		{Prefix: map[int]byte{0: 'X'}, PF: 3}, // invalid prefix on the save press
		{PF: 12},                              // then cancel
	}}
	saveCalled := false
	err := RunEditor(context.Background(), f, EditorConfig{
		Title: "EDIT MOTD", Rows: 24, Lines: []string{"A", "B"},
		Save: func(_ context.Context, _ []string) (string, error) {
			saveCalled = true
			return "", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saveCalled {
		t.Error("Save called; an invalid prefix in the same transmission must veto PF3")
	}
	if got := f.editorViews[1].ErrMsg; !strings.Contains(got, "INVALID LINE COMMAND") {
		t.Errorf("second paint ErrMsg = %q, want it to contain INVALID LINE COMMAND", got)
	}
}

func TestRunEditorEmptyDocGetsOneLine(t *testing.T) {
	f := &fakeRenderer{editorActs: []EditorAction{{PF: 12}}}
	if err := RunEditor(context.Background(), f, EditorConfig{
		Title: "EDIT MOTD", Rows: 24, Lines: nil,
		Save: func(_ context.Context, _ []string) (string, error) { return "", nil },
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.editorViews[0].Lines; len(got) != 1 || got[0].Text != "" {
		t.Errorf("first paint of empty doc: %+v, want one empty line", got)
	}
}

func TestRunEditorWideLineProtectedAndTextIgnored(t *testing.T) {
	wide := strings.Repeat("W", 80)
	f := &fakeRenderer{editorActs: []EditorAction{
		// A hostile/buggy client returns text for the protected line anyway.
		{Text: map[int]string{0: "clobber"}, PF: 3},
	}}
	var saved []string
	if err := RunEditor(context.Background(), f, EditorConfig{
		Title: "EDIT BRANDING", Rows: 24, Lines: []string{wide},
		Save: func(_ context.Context, lines []string) (string, error) {
			saved = slices.Clone(lines)
			return "", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if !f.editorViews[0].Lines[0].Protected {
		t.Error("wide line not marked Protected")
	}
	if saved[0] != wide {
		t.Errorf("wide line content changed: %q", saved[0])
	}
}

func TestRunEditorSaveErrStaysOnScreen(t *testing.T) {
	f := &fakeRenderer{editorActs: []EditorAction{{PF: 3}, {PF: 12}}}
	calls := 0
	if err := RunEditor(context.Background(), f, EditorConfig{
		Title: "EDIT MOTD", Rows: 24, Lines: []string{"A"},
		Save: func(_ context.Context, _ []string) (string, error) {
			calls++
			return "DOCUMENT TOO LARGE (MAX 8 KIB)", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(f.editorViews) != 2 || f.editorViews[1].ErrMsg == "" {
		t.Errorf("calls=%d paints=%d errmsg=%q; save error must re-present",
			calls, len(f.editorViews), f.editorViews[1].ErrMsg)
	}
}

func TestRunEditorPaging(t *testing.T) {
	// 20 lines, page size 14 on a 24-row screen: PF8 shows lines 14..19.
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("L%02d", i)
	}
	f := &fakeRenderer{editorActs: []EditorAction{{PF: 8}, {PF: 12}}}
	if err := RunEditor(context.Background(), f, EditorConfig{
		Title: "EDIT MOTD", Rows: 24, Lines: lines,
		Save: func(_ context.Context, _ []string) (string, error) { return "", nil },
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.editorViews[1].Lines[0].Text; got != "L14" {
		t.Errorf("page 2 first line %q, want L14", got)
	}
	if f.editorViews[1].RowInfo == "" {
		t.Error("RowInfo empty; want ROW x TO y OF z")
	}
}

func TestApplyPrefix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      []string
		cmds    map[int]byte
		want    []string
		wantErr bool
	}{
		{"insert", []string{"a", "b"}, map[int]byte{0: 'I'}, []string{"a", "", "b"}, false},
		{"delete", []string{"a", "b", "c"}, map[int]byte{1: 'D'}, []string{"a", "c"}, false},
		{"repeat", []string{"a", "b"}, map[int]byte{0: 'R'}, []string{"a", "a", "b"}, false},
		{"multiple same transmission", []string{"a", "b", "c"}, map[int]byte{0: 'D', 2: 'I'},
			[]string{"b", "c", ""}, false},
		{"delete last line leaves one empty", []string{"a"}, map[int]byte{0: 'D'}, []string{""}, false},
		{"invalid command vetoes all", []string{"a", "b"}, map[int]byte{0: 'D', 1: 'X'},
			[]string{"a", "b"}, true},
		{"out of range ignored", []string{"a"}, map[int]byte{5: 'D'}, []string{"a"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, errMsg := applyPrefix(slices.Clone(tc.in), tc.cmds)
			if (errMsg != "") != tc.wantErr {
				t.Fatalf("errMsg = %q, wantErr=%v", errMsg, tc.wantErr)
			}
			if !tc.wantErr && !slices.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
