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
	"testing"

	"github.com/racingmars/go3270"
)

// fieldAt returns the field whose attribute byte is at (row, col), or false.
func fieldAt(s go3270.Screen, row, col int) (go3270.Field, bool) {
	for _, f := range s {
		if f.Row == row && f.Col == col {
			return f, true
		}
	}
	return go3270.Field{}, false
}

func TestBuildSnapshotScreen_RowLayoutAndColor(t *testing.T) {
	v := SnapshotView{
		Title: "RECENT ACTIVITY", RowInfo: "ROW 1 TO 1 OF 1",
		AsOf: "AS OF 2026-06-06 (2026.157)  14:32 UTC",
		Head: SnapshotRow{Left: "MM/DD HH:MM USERNAME", Mid: "EVENT", Right: "DETAIL"},
		Rows: []SnapshotRow{
			{Left: "06/06 14:28 BADGUY", Mid: "AUTH_FAIL", Right: "delay=4s count=2", MidColor: go3270.Red},
		},
		Legend: "S=Detail", PFHelp: "PF3=Back   PF7=Bkwd  PF8=Fwd   Enter=Refresh",
		Empty:  "(none)",
	}
	screen, cur := buildSnapshotScreen(24, v)

	if f, ok := fieldAt(screen, 0, centerCol(len("RECENT ACTIVITY"))); !ok || f.Content != "RECENT ACTIVITY" || f.Color != go3270.White || !f.Intense {
		t.Errorf("title missing/not centered-white: %+v ok=%v", f, ok)
	}
	if f, ok := fieldAt(screen, 1, centerCol(len(v.AsOf))); !ok || f.Content != v.AsOf || f.Color != go3270.Turquoise {
		t.Errorf("as-of stamp missing/not centered-turquoise on row 1: %+v ok=%v", f, ok)
	}
	if f, ok := fieldAt(screen, 3, snapLeftAttr); !ok || f.Color != go3270.Blue {
		t.Errorf("heading-left not blue on row 3: %+v ok=%v", f, ok)
	}
	if f, ok := fieldAt(screen, 4, snapCmdAttr); !ok || !f.Write {
		t.Errorf("cmd field missing/not writable on data row: %+v ok=%v", f, ok)
	}
	if f, ok := fieldAt(screen, 4, snapMidAttr); !ok || f.Content != "AUTH_FAIL" || f.Color != go3270.Red {
		t.Errorf("event field wrong: %+v ok=%v", f, ok)
	}
	if f, ok := fieldAt(screen, 4, snapRightAttr); !ok || f.Content != "delay=4s count=2" {
		t.Errorf("detail field wrong: %+v ok=%v", f, ok)
	}
	if cur.Row != 4 || cur.Col != snapCmdAttr+1 {
		t.Errorf("cursor = %+v, want {4,%d}", cur, snapCmdAttr+1)
	}
}

func TestBuildSnapshotScreen_Empty(t *testing.T) {
	screen, cur := buildSnapshotScreen(24, SnapshotView{Empty: "(none)"})
	if f, ok := fieldAt(screen, 4, snapLeftAttr); !ok || f.Content != "(none)" {
		t.Errorf("empty marker missing: %+v ok=%v", f, ok)
	}
	if cur != (Cursor{Row: 0, Col: 0}) {
		t.Errorf("empty cursor = %+v, want home {0,0}", cur)
	}
}

func TestWrapText(t *testing.T) {
	got := wrapText("alpha beta gamma delta", 11)
	want := []string{"alpha beta", "gamma delta"}
	if len(got) != len(want) {
		t.Fatalf("wrapText lines = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
	// A single token longer than width is hard-split, never dropped.
	if g := wrapText("abcdefgh", 3); len(g) != 3 || g[0] != "abc" {
		t.Errorf("hard-split = %v", g)
	}
	if g := wrapText("", 10); len(g) != 0 {
		t.Errorf("empty = %v, want no lines", g)
	}
}

func TestBuildDetailScreen(t *testing.T) {
	v := DetailView{
		Title: "AUDIT DETAIL",
		Fields: []DetailField{
			{Label: "Date/Time", Value: "2026-06-06 (2026.157) 14:28:07 UTC"},
			{Label: "Event", Value: "AUTH_FAIL", Color: go3270.Red},
		},
		BodyLabel: "Detail", Body: "delay=4s count=2",
		PFHelp: "PF3=Back",
	}
	screen, cur := buildDetailScreen(24, v)

	if f, ok := fieldAt(screen, 0, centerCol(len("AUDIT DETAIL"))); !ok || f.Content != "AUDIT DETAIL" || f.Color != go3270.White || !f.Intense {
		t.Errorf("title not centered-white: %+v ok=%v", f, ok)
	}
	if f, ok := fieldAt(screen, 2, 2); !ok || f.Content != "Date/Time" {
		t.Errorf("first label wrong: %+v ok=%v", f, ok)
	}
	if f, ok := fieldAt(screen, 3, detailValueCol); !ok || f.Content != "AUTH_FAIL" || f.Color != go3270.Red {
		t.Errorf("event value wrong/uncoloured: %+v ok=%v", f, ok)
	}
	if cur != (Cursor{Row: 0, Col: 0}) {
		t.Errorf("cursor = %+v, want home", cur)
	}
}

func TestBuildSnapshotScreen_WideRendersFullWidthRows(t *testing.T) {
	v := SnapshotView{
		Title: "ACTIVE SESSIONS",
		AsOf:  "AS OF X",
		Wide:  true,
		Head:  SnapshotRow{Left: "ID    CLIENT"},
		Rows:  []SnapshotRow{{Left: "12    1.2.3.4:5"}},
	}
	screen, _ := buildSnapshotScreen(24, v)

	var sawWide, sawMid bool
	for _, f := range screen {
		if f.Row == 4 && f.Col == snapLeftAttr && f.Content == "12    1.2.3.4:5" {
			sawWide = true
		}
		if f.Row == 4 && f.Col == snapMidAttr {
			sawMid = true
		}
	}
	if !sawWide {
		t.Error("wide data row not rendered as a single left-anchored field")
	}
	if sawMid {
		t.Error("wide mode must not emit a Mid-segment field (it would chop the row)")
	}

	// The heading row (bodyTopRow) must also be a single full-width field, with
	// no Mid/Right segment fields that would chop the packed columns.
	if f, ok := fieldAt(screen, bodyTopRow(), snapLeftAttr); !ok || f.Content != "ID    CLIENT" {
		t.Errorf("wide heading not rendered as a single left-anchored field: %+v ok=%v", f, ok)
	}
	for _, f := range screen {
		if f.Row == bodyTopRow() && (f.Col == snapMidAttr || f.Col == snapRightAttr) {
			t.Error("wide mode must not emit Mid/Right heading fields")
		}
	}
}

func TestBuildDetailScreen_DotLeader(t *testing.T) {
	v := DetailView{
		Title:     "AUDIT DETAIL",
		Fields:    []DetailField{{Label: "PTR", Value: "host.example"}},
		PFHelp:    "PF3=Back",
		DotLeader: true,
	}
	screen, _ := buildDetailScreen(24, v)

	// Label is dot-leadered to the form width (colon right-aligned just before
	// the value column) instead of the plain "PTR".
	want := dotLeaderLabel("PTR", formLabelMax(detailValueCol))
	if f, ok := fieldAt(screen, 2, labelAttrCol); !ok || f.Content != want {
		t.Errorf("dot-leader label = %q ok=%v, want %q", f.Content, ok, want)
	}
}
