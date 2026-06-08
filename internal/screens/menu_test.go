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
	screen, mapping, _ := MenuScreen(DefaultGeometry, svcs, false, false, MenuStatus{}, "", 0)

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
	screen, mapping, _ := MenuScreen(DefaultGeometry, nil, false, false, MenuStatus{}, "", 0)
	if len(mapping) != 0 {
		t.Errorf("mapping should be empty, got %d", len(mapping))
	}
	if _, ok := fieldByName(screen, FieldSelection); !ok {
		t.Errorf("missing %q field", FieldSelection)
	}
}

func TestMenuScreenShowsError(t *testing.T) {
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, false, MenuStatus{}, "Backend unreachable", 0)
	f, ok := fieldByName(screen, FieldError)
	if !ok {
		t.Fatalf("missing error field")
	}
	if f.Content != "Backend unreachable" {
		t.Errorf("error content = %q", f.Content)
	}
}

func TestMenuScreenAdminEntry(t *testing.T) {
	screen, mapping, _ := MenuScreen(DefaultGeometry, nil, true, false, MenuStatus{}, "", 0)
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

	screen, _, _ = MenuScreen(DefaultGeometry, nil, false, false, MenuStatus{}, "", 0)
	if screenContains(screen, "Administration") {
		t.Errorf("non-admin must not see the admin entry")
	}
	f, _ = fieldByName(screen, FieldSelection)
	if !f.NumericOnly {
		t.Errorf("non-admin selection stays numeric-only")
	}
}

func TestMenuScreenAdminEntryClampedWithManyServices(t *testing.T) {
	g := DefaultGeometry
	svcs := make([]store.Service, 16)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i), Host: "h", Port: 23}
	}
	screen, _, _ := MenuScreen(g, svcs, true, false, MenuStatus{}, "", 0)
	for _, f := range screen {
		if strings.Contains(f.Content, "Administration") && f.Row > g.BodyBottomRow() {
			t.Errorf("admin entry at row %d beyond body bottom %d", f.Row, g.BodyBottomRow())
		}
	}
}

func TestMenuScreenHelpSaysLogoff(t *testing.T) {
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, false, MenuStatus{}, "", 0)
	if !screenContains(screen, "PF3=Logoff") {
		t.Errorf("menu help should say PF3=Logoff")
	}
}

func TestMenuScreenTopBand(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, _, cur := MenuScreen(g, nil, false, false, MenuStatus{}, "oops", 0)

	sel, ok := fieldByName(screen, FieldSelection)
	if !ok {
		t.Fatal("missing selection field")
	}
	if sel.Row != g.CommandRow() || sel.Col != 14 {
		t.Errorf("selection = row %d col %d, want row %d col 14", sel.Row, sel.Col, g.CommandRow())
	}
	if cur.Row != 1 || cur.Col != 15 {
		t.Errorf("cursor = %+v, want (1,15)", cur)
	}
	msg, _ := fieldByName(screen, FieldError)
	if msg.Row != g.MessageRow() {
		t.Errorf("message row = %d, want %d", msg.Row, g.MessageRow())
	}
	instr, ok := fieldByContent(screen, "Select a service and press ENTER:")
	if !ok {
		t.Fatal("missing instruction line")
	}
	if instr.Row != g.BodyTopRow() || instr.Color != go3270.Turquoise {
		t.Errorf("instruction = row %d color %v, want row %d turquoise", instr.Row, instr.Color, g.BodyTopRow())
	}
}

func TestMenuScreenBandsAcrossGeometries(t *testing.T) {
	for _, g := range []Geometry{{Rows: 24, Cols: 80}, {Rows: 32, Cols: 80}, {Rows: 43, Cols: 80}} {
		screen, _, _ := MenuScreen(g, nil, false, false, MenuStatus{}, "err", 0)
		sel, ok := fieldByName(screen, FieldSelection)
		if !ok || sel.Row != g.CommandRow() {
			t.Errorf("%+v: selection row = %d, want CommandRow %d", g, sel.Row, g.CommandRow())
		}
		e, _ := fieldByName(screen, FieldError)
		if e.Row != g.MessageRow() {
			t.Errorf("%+v: error row = %d, want MessageRow %d", g, e.Row, g.MessageRow())
		}
	}
}

func TestMenuScreenMappingCoversAllServices(t *testing.T) {
	svcs := make([]store.Service, 30)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i), Host: "h", Port: 23}
	}

	// Mapping is global: every service is selectable by number regardless of
	// page. The rendered page window must never touch the help row or below.
	g2 := Geometry{Rows: 24, Cols: 80}
	screen, mapping, _ := MenuScreen(g2, svcs, false, false, MenuStatus{}, "", 0)
	if len(mapping) != len(svcs) {
		t.Errorf("MOD 2 mapping = %d entries, want %d (full list)", len(mapping), len(svcs))
	}
	for _, f := range screen {
		if f.Row > g2.HelpRow() {
			t.Errorf("MOD 2: field beyond help row %d: %+v", g2.HelpRow(), f)
		}
	}

	// MOD 3 has the same full mapping (paging, not truncation).
	g3 := Geometry{Rows: 32, Cols: 80}
	_, mapping, _ = MenuScreen(g3, svcs, false, false, MenuStatus{}, "", 0)
	if len(mapping) != len(svcs) {
		t.Errorf("MOD 3 mapping = %d entries, want %d (full list)", len(mapping), len(svcs))
	}
}

func TestMenuRendersISPFStyleAndHidesHostPort(t *testing.T) {
	svcs := []store.Service{
		{Name: "PRODCICS", Description: "Production CICS Region", Host: "secret.internal", Port: 992},
	}
	screen, mapping, _ := MenuScreen(DefaultGeometry, svcs, false, false, MenuStatus{}, "", 0)
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
	screen, _, cur := MenuScreen(DefaultGeometry, nil, false, false, MenuStatus{}, "", 0)
	sf, ok := fieldByName(screen, FieldSelection)
	if !ok {
		t.Fatalf("missing %q field", FieldSelection)
	}
	if want := cursorAt(sf); cur != want {
		t.Errorf("menu cursor = %+v, want %+v (selection field row %d col %d)", cur, want, sf.Row, sf.Col)
	}
}

func TestMenuScreenAdminEntryNeverCollidesWhenFull(t *testing.T) {
	svcs := make([]store.Service, 50)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i), Host: "h", Port: 23}
	}
	for _, g := range []Geometry{{Rows: 24, Cols: 80}, {Rows: 43, Cols: 80}} {
		screen, mapping, _ := MenuScreen(g, svcs, true, false, MenuStatus{}, "", 0)
		if len(mapping) != len(svcs) { // global mapping covers every service
			t.Errorf("%+v: admin mapping = %d entries, want %d", g, len(mapping), len(svcs))
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
		if adminRow < 0 || adminRow > g.BodyBottomRow() {
			t.Errorf("%+v: admin entry row = %d, want ≤ %d", g, adminRow, g.BodyBottomRow())
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
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, false, status, "", 0)

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
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, false, status, "", 0)
	if !hasContent(screen, "AVERYLO") { // username upper, cut to 7
		t.Errorf("username not cut to 7 runes")
	}
	if !hasContent(screen, "0123456") { // release cut to 7
		t.Errorf("release not cut to 7 runes")
	}
}

func TestMenuScreenGridDescription(t *testing.T) {
	svcs := []store.Service{{ID: 1, Name: "CICSPROD", Description: "CICS TS Prod"}}
	screen, _, _ := MenuScreen(DefaultGeometry, svcs, false, false, MenuStatus{}, "", 0)
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
	screen, _, _ := MenuScreen(Geometry{}, nil, false, false, MenuStatus{}, "", 0)
	if !screenContains(screen, "User Settings") {
		t.Errorf("menu missing '0 User Settings' meta entry")
	}
}

func TestMenuGridColumns(t *testing.T) {
	// Tri-color grid spacing (GH #65 QA): number col 0, name col 6, description
	// col 17 — three blank columns at each gap.
	svcs := []store.Service{{ID: 1, Name: "PRODCICS", Description: "Production CICS Region"}}
	screen, _, _ := MenuScreen(DefaultGeometry, svcs, false, false, MenuStatus{}, "", 0)
	name, ok := fieldByContent(screen, "PRODCICS")
	if !ok || name.Col != 6 {
		t.Errorf("service name col = %d ok=%v, want 6", name.Col, ok)
	}
	desc, ok := fieldByContent(screen, "Production CICS Region")
	if !ok || desc.Col != 17 {
		t.Errorf("service desc col = %d ok=%v, want 17", desc.Col, ok)
	}
	us, ok := fieldByContent(screen, "User Settings")
	if !ok || us.Col != 17 {
		t.Errorf("meta label col = %d ok=%v, want 17", us.Col, ok)
	}
}

func TestMenuScreenPagesWithGlobalNumbers(t *testing.T) {
	svcs := make([]store.Service, 22)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i+1), Host: "h", Port: 23}
	}
	g := DefaultGeometry // capacity 17 non-admin

	// Page 0: shows global numbers 1..17, the page-1 indicator, and NOT item 18.
	p0, mapping, _ := MenuScreen(g, svcs, false, false, MenuStatus{}, "", 0)
	if len(mapping) != 22 {
		t.Fatalf("mapping = %d, want 22 (full list)", len(mapping))
	}
	if mapping["18"].Name != "SVC18" {
		t.Errorf(`mapping["18"] = %q, want SVC18 (off-page but selectable)`, mapping["18"].Name)
	}
	if !hasContent(p0, fmt.Sprintf("%3d", 1)) {
		t.Errorf("page 0 missing global number 1")
	}
	if hasContent(p0, fmt.Sprintf("%3d", 18)) {
		t.Errorf("page 0 should not render global number 18")
	}
	if !hasContent(p0, "ITEMS 1 TO 17 OF 22") {
		t.Errorf("page 0 missing indicator 'ITEMS 1 TO 17 OF 22'")
	}

	// Page 1: shows global numbers 18..22, the page-2 indicator, and NOT item 1.
	p1, _, _ := MenuScreen(g, svcs, false, false, MenuStatus{}, "", 1)
	if !hasContent(p1, fmt.Sprintf("%3d", 18)) {
		t.Errorf("page 1 missing global number 18")
	}
	if hasContent(p1, fmt.Sprintf("%3d", 1)) {
		t.Errorf("page 1 should not render global number 1")
	}
	if !hasContent(p1, "ITEMS 18 TO 22 OF 22") {
		t.Errorf("page 1 missing indicator 'ITEMS 18 TO 22 OF 22'")
	}
}

func TestMenuScreenMetaBandOnEveryPage(t *testing.T) {
	svcs := make([]store.Service, 22)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i+1), Host: "h", Port: 23}
	}
	g := DefaultGeometry
	for _, page := range []int{0, 1} {
		screen, _, _ := MenuScreen(g, svcs, true, false, MenuStatus{}, "", page)
		if !screenContains(screen, "User Settings") {
			t.Errorf("page %d missing '0 User Settings' meta entry", page)
		}
		if !screenContains(screen, "Administration") {
			t.Errorf("page %d missing 'A Administration' meta entry", page)
		}
		// Meta band is bottom-anchored: "0" one above menuBottomRow, "A" on it.
		us, _ := fieldByContent(screen, "User Settings")
		if us.Row != g.menuBottomRow()-1 {
			t.Errorf("page %d: User Settings row = %d, want %d", page, us.Row, g.menuBottomRow()-1)
		}
		admin, _ := fieldByContent(screen, "Administration")
		if admin.Row != g.menuBottomRow() {
			t.Errorf("page %d: Administration row = %d, want %d", page, admin.Row, g.menuBottomRow())
		}
	}
}

func TestMenuScreenBlankSeparatorAboveHelp(t *testing.T) {
	svcs := make([]store.Service, 22)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i+1), Host: "h", Port: 23}
	}
	g := DefaultGeometry
	screen, _, _ := MenuScreen(g, svcs, true, false, MenuStatus{}, "", 0)
	sep := g.BodyBottomRow() // the row left blank between content and PF legend
	for _, f := range screen {
		if f.Row == sep && f.Content != "" {
			t.Errorf("separator row %d should be blank, found %+v", sep, f)
		}
	}
}

func TestMenuScreenHelpListsPagingKeys(t *testing.T) {
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, false, MenuStatus{}, "", 0)
	if !screenContains(screen, "PF7") || !screenContains(screen, "PF8") {
		t.Errorf("menu help should list PF7/PF8 paging keys")
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

func TestMenuScreenLockedHidesUserSettings(t *testing.T) {
	// non-admin, locked: neither "0 User Settings" nor "Administration".
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, true, MenuStatus{}, "", 0)
	if screenContains(screen, "User Settings") {
		t.Error("locked non-admin must not see User Settings entry")
	}
	if screenContains(screen, "Administration") {
		t.Error("non-admin must never see Administration")
	}

	// non-admin, unlocked: User Settings present (regression).
	screen, _, _ = MenuScreen(DefaultGeometry, nil, false, false, MenuStatus{}, "", 0)
	if !screenContains(screen, "User Settings") {
		t.Error("unlocked non-admin must see User Settings entry")
	}

	// admin, locked: Administration present, User Settings hidden.
	screen, _, _ = MenuScreen(DefaultGeometry, nil, true, true, MenuStatus{}, "", 0)
	if screenContains(screen, "User Settings") {
		t.Error("locked admin must not see User Settings entry")
	}
	if !screenContains(screen, "Administration") {
		t.Error("locked admin must still see Administration")
	}

	// admin, unlocked: both present.
	screen, _, _ = MenuScreen(DefaultGeometry, nil, true, false, MenuStatus{}, "", 0)
	if !screenContains(screen, "User Settings") || !screenContains(screen, "Administration") {
		t.Error("unlocked admin must see both entries")
	}
}

// TestMenuScreenLockedHidesUserSettingsAcrossPages guards against the "0" entry
// reappearing on a non-first page when locked (the meta band is rendered every
// page). Uses enough services to force a second page.
func TestMenuScreenLockedHidesUserSettingsAcrossPages(t *testing.T) {
	g := DefaultGeometry
	svcs := make([]store.Service, 2*g.MenuCapacity(false)+1)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i), Host: "h", Port: 23}
	}
	// page 1, locked: still no "0" entry.
	screen, _, _ := MenuScreen(g, svcs, false, true, MenuStatus{}, "", 1)
	if screenContains(screen, "User Settings") {
		t.Error("locked user must not see User Settings entry on page 1")
	}
	// page 1, unlocked: "0" entry still present (regression).
	screen, _, _ = MenuScreen(g, svcs, false, false, MenuStatus{}, "", 1)
	if !screenContains(screen, "User Settings") {
		t.Error("unlocked user must still see User Settings entry on page 1")
	}
}
