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

package screens

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/racingmars/go3270"
)

func TestMenuScreenMapping(t *testing.T) {
	svcs := []store.Service{
		{ID: 1, Name: "PROD CICS", Host: "prod", Port: 23},
		{ID: 2, Name: "TEST CICS", Host: "test", Port: 992, TLS: true},
	}
	screen, mapping, _ := MenuScreen(DefaultGeometry, svcs, false, MenuStatus{}, "")

	if len(mapping) != 2 {
		t.Fatalf("mapping has %d entries, want 2", len(mapping))
	}
	if mapping["1"].Name != "PROD CICS" {
		t.Errorf(`mapping["1"] = %+v, want PROD CICS`, mapping["1"])
	}
	if mapping["2"].Name != "TEST CICS" {
		t.Errorf(`mapping["2"] = %+v, want TEST CICS`, mapping["2"])
	}

	if _, ok := fieldByName(screen, FieldSelection); !ok {
		t.Errorf("missing %q field", FieldSelection)
	}
}

func TestMenuScreenEmpty(t *testing.T) {
	screen, mapping, _ := MenuScreen(DefaultGeometry, nil, false, MenuStatus{}, "")
	if len(mapping) != 0 {
		t.Errorf("mapping should be empty, got %d", len(mapping))
	}
	if _, ok := fieldByName(screen, FieldSelection); !ok {
		t.Errorf("missing %q field", FieldSelection)
	}
}

func TestMenuScreenShowsError(t *testing.T) {
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, MenuStatus{}, "Backend unreachable")
	f, ok := fieldByName(screen, FieldError)
	if !ok {
		t.Fatalf("missing error field")
	}
	if f.Content != "Backend unreachable" {
		t.Errorf("error content = %q", f.Content)
	}
}

func TestMenuScreenAdminEntry(t *testing.T) {
	screen, mapping, _ := MenuScreen(DefaultGeometry, nil, true, MenuStatus{}, "")
	if len(mapping) != 0 {
		t.Errorf("mapping = %v, want empty (admin entry is not a service)", mapping)
	}
	// New grid: number and label are separate fields.
	if !screenContains(screen, "Administration") {
		t.Errorf("missing admin entry")
	}
	f, ok := fieldByName(screen, FieldSelection)
	if !ok || f.NumericOnly {
		t.Errorf("selection field must accept 'A' for admins: %+v", f)
	}

	screen, _, _ = MenuScreen(DefaultGeometry, nil, false, MenuStatus{}, "")
	if screenContains(screen, "Administration") {
		t.Errorf("non-admin must not see the admin entry")
	}
	f, _ = fieldByName(screen, FieldSelection)
	if !f.NumericOnly {
		t.Errorf("non-admin selection stays numeric-only")
	}
}

func TestMenuScreenAdminEntryClampedWithManyServices(t *testing.T) {
	svcs := make([]store.Service, 16)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i), Host: "h", Port: 23}
	}
	screen, _, _ := MenuScreen(DefaultGeometry, svcs, true, MenuStatus{}, "")
	for _, f := range screen {
		if strings.Contains(f.Content, "Administration") && f.Row > 17 {
			t.Errorf("admin entry at row %d would collide with the input line", f.Row)
		}
	}
}

func TestMenuScreenHelpSaysLogoff(t *testing.T) {
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, MenuStatus{}, "")
	if !screenContains(screen, "PF3=Logoff") {
		t.Errorf("menu help should say PF3=Logoff")
	}
}

func TestMenuScreenBottomAnchored(t *testing.T) {
	for _, g := range []Geometry{{Rows: 24, Cols: 80}, {Rows: 32, Cols: 80}, {Rows: 43, Cols: 80}} {
		screen, _, _ := MenuScreen(g, nil, false, MenuStatus{}, "err")
		sel, ok := fieldByName(screen, FieldSelection)
		if !ok || sel.Row != g.InputRow() {
			t.Errorf("%+v: selection row = %d, want %d", g, sel.Row, g.InputRow())
		}
		e, _ := fieldByName(screen, FieldError)
		if e.Row != g.ErrorRow() {
			t.Errorf("%+v: error row = %d, want %d", g, e.Row, g.ErrorRow())
		}
	}
}

func TestMenuScreenCapacityGrowsAndTruncates(t *testing.T) {
	svcs := make([]store.Service, 30)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i), Host: "h", Port: 23}
	}

	// MOD 2: truncated to capacity; nothing may touch the input row or below.
	g2 := Geometry{Rows: 24, Cols: 80}
	screen, mapping, _ := MenuScreen(g2, svcs, false, MenuStatus{}, "")
	if len(mapping) != g2.MenuCapacity(false) {
		t.Errorf("MOD 2 mapping = %d entries, want %d", len(mapping), g2.MenuCapacity(false))
	}
	for _, f := range screen {
		if f.Row > g2.InputRow() && f.Row != g2.ErrorRow() && f.Row != g2.HelpRow() {
			t.Errorf("MOD 2: unexpected field on row %d: %+v", f.Row, f)
		}
	}

	// MOD 3: more services fit.
	g3 := Geometry{Rows: 32, Cols: 80}
	_, mapping, _ = MenuScreen(g3, svcs, false, MenuStatus{}, "")
	if len(mapping) != g3.MenuCapacity(false) {
		t.Errorf("MOD 3 mapping = %d entries, want %d", len(mapping), g3.MenuCapacity(false))
	}
}

func TestMenuRendersISPFStyleAndHidesHostPort(t *testing.T) {
	svcs := []store.Service{
		{Name: "PRODCICS", Description: "Production CICS Region", Host: "secret.internal", Port: 992},
	}
	screen, mapping, _ := MenuScreen(DefaultGeometry, svcs, false, MenuStatus{}, "")
	if _, ok := mapping["1"]; !ok {
		t.Fatal("selection 1 not mapped")
	}
	// New grid: name and description are separate fields on the same row.
	var foundName, foundDesc bool
	for _, f := range screen {
		if strings.Contains(f.Content, "PRODCICS") {
			foundName = true
		}
		if strings.Contains(f.Content, "Production CICS Region") {
			foundDesc = true
		}
		if strings.Contains(f.Content, "secret.internal") || strings.Contains(f.Content, "992") {
			t.Errorf("menu leaks host/port: %q", f.Content)
		}
	}
	if !foundName || !foundDesc {
		t.Error("expected ISPF-style grid with separate NAME and Description fields")
	}
}

func TestMenuScreenCursor(t *testing.T) {
	screen, _, cur := MenuScreen(DefaultGeometry, nil, false, MenuStatus{}, "")
	sf, ok := fieldByName(screen, FieldSelection)
	if !ok {
		t.Fatalf("missing %q field", FieldSelection)
	}
	if want := cursorAt(sf); cur != want {
		t.Errorf("menu cursor = %+v, want %+v (selection field row %d col %d)", cur, want, sf.Row, sf.Col)
	}
}

func TestMenuScreenAdminEntryNeverCollidesWhenFull(t *testing.T) {
	svcs := make([]store.Service, 32)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i), Host: "h", Port: 23}
	}
	for _, g := range []Geometry{{Rows: 24, Cols: 80}, {Rows: 43, Cols: 80}} {
		screen, mapping, _ := MenuScreen(g, svcs, true, MenuStatus{}, "")
		if len(mapping) != g.MenuCapacity(true) { // one less: row reserved for A entry
			t.Errorf("%+v: admin mapping = %d entries, want %d", g, len(mapping), g.MenuCapacity(true))
		}
		adminRow := -1
		occupied := map[int]int{}
		for _, f := range screen {
			if strings.Contains(f.Content, "Administration") {
				adminRow = f.Row
			}
			if f.Content != "" {
				occupied[f.Row]++
			}
		}
		if adminRow < 0 || adminRow > g.InputRow()-2 {
			t.Errorf("%+v: admin entry row = %d, want ≤ %d", g, adminRow, g.InputRow()-2)
		}
		// New grid: admin row has 2 fields ("  A" number + "Administration" label).
		// More than 2 means a service row is colliding with the admin row.
		if occupied[adminRow] != 2 {
			t.Errorf("%+v: admin entry row %d has %d fields, want exactly 2", g, adminRow, occupied[adminRow])
		}
	}
}

func TestMenuScreenStatusBlock(t *testing.T) {
	status := MenuStatus{
		Username: "merlin",
		TermType: "IBM-3279-2",
		SystemID: "PROXY",
		Release:  "v0.8.0",
		Now:      time.Date(2026, 6, 5, 21, 14, 0, 0, time.UTC),
	}
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, status, "")

	// The block shows even on the no-services screen. Assert content (not
	// columns — columns are verified by the s3270 smoke script).
	wantContents := []string{
		"User ID. :", "MERLIN", // upper-cased
		"Date . . :", "26.156",
		"Time . . :", "21:14",
		"Terminal :", "3279-2", // IBM- stripped
		"System ID:", "PROXY",
		"Release. :", "v0.8.0",
	}
	for _, want := range wantContents {
		if !hasContent(screen, want) {
			t.Errorf("status block missing content %q", want)
		}
	}
}

func TestMenuScreenStatusBlockTruncates(t *testing.T) {
	status := MenuStatus{
		Username: "averylongusername",
		Release:  "0123456789ab", // 12-char VCS-style rev
		Now:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, status, "")
	if !hasContent(screen, "AVERYLO") { // username upper, cut to 7
		t.Errorf("username not cut to 7 runes")
	}
	if !hasContent(screen, "0123456") { // release cut to 7
		t.Errorf("release not cut to 7 runes")
	}
}

func TestMenuScreenGridDescription(t *testing.T) {
	svcs := []store.Service{{ID: 1, Name: "CICSPROD", Description: "CICS TS Prod"}}
	screen, _, _ := MenuScreen(DefaultGeometry, svcs, false, MenuStatus{}, "")
	// Name and description are now separate fields (separately colored grid),
	// not one combined string.
	if !hasContent(screen, "CICSPROD") {
		t.Errorf("service name field missing")
	}
	if !hasContent(screen, "CICS TS Prod") {
		t.Errorf("service description field missing")
	}
}

func TestMenuShowsUserSettingsEntry(t *testing.T) {
	screen, _, _ := MenuScreen(Geometry{}, nil, false, MenuStatus{}, "")
	if !screenContains(screen, "User Settings") {
		t.Errorf("menu missing '0 User Settings' meta entry")
	}
}

// hasContent reports whether any field's Content equals want.
func hasContent(screen go3270.Screen, want string) bool {
	for _, f := range screen {
		if f.Content == want {
			return true
		}
	}
	return false
}
