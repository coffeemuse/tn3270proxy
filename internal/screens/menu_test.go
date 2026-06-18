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

	"github.com/coffeemuse/tn3270proxy/internal/store"
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

func TestMenuScreenHelpAdvertisesKeys(t *testing.T) {
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, false, MenuStatus{}, "", 0)
	for _, want := range []string{"PF1=Help", "PF3=Logoff", "PF7=PgUp", "PF8=PgDn", "PA3 returns here"} {
		if !screenContains(screen, want) {
			t.Errorf("menu help row should contain %q", want)
		}
	}
}

func TestMenuScreenTopBand(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, _, cur := MenuScreen(g, nil, false, false, MenuStatus{}, "oops", 0)

	sel, ok := fieldByName(screen, FieldSelection)
	if !ok {
		t.Fatal("missing selection field")
	}
	if sel.Row != g.CommandRow() || sel.Col != 12 {
		t.Errorf("selection = row %d col %d, want row %d col 12", sel.Row, sel.Col, g.CommandRow())
	}
	if cur.Row != 1 || cur.Col != 13 {
		t.Errorf("cursor = %+v, want (1,13)", cur)
	}
	msg, _ := fieldByName(screen, FieldError)
	if msg.Row != g.MessageRow() {
		t.Errorf("message row = %d, want %d", msg.Row, g.MessageRow())
	}
	// The "Select a service and press ENTER:" instruction line was removed
	// (GH #130); the service list now starts at BodyTopRow().
	if screenContains(screen, "Select a service and press ENTER:") {
		t.Error("obsolete instruction line still present")
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
		// New grid: admin row has 3 fields ("  A" number + "Admin" name +
		// "System Administration" label). More than 3 means a service row is
		// colliding with the admin row.
		if occupied[adminRow] != 3 {
			t.Errorf("%+v: admin entry row %d has %d fields, want exactly 3", g, adminRow, occupied[adminRow])
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

func TestMenuShowsSettingsEntry(t *testing.T) {
	screen, _, _ := MenuScreen(Geometry{}, nil, false, false, MenuStatus{}, "", 0)
	if !screenContains(screen, "Settings") {
		t.Errorf("menu missing '0 Settings' option")
	}
	if !screenContains(screen, "User and security parameters") {
		t.Errorf("menu missing Settings description")
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
	sName, ok := fieldByContent(screen, "Settings")
	if !ok || sName.Col != 6 {
		t.Errorf("settings name col = %d ok=%v, want 6", sName.Col, ok)
	}
	sDesc, ok := fieldByContent(screen, "User and security parameters")
	if !ok || sDesc.Col != 17 {
		t.Errorf("settings description col = %d ok=%v, want 17", sDesc.Col, ok)
	}
}

func TestMenuScreenPagesWithGlobalNumbers(t *testing.T) {
	svcs := make([]store.Service, 22)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i+1), Host: "h", Port: 23}
	}
	g := DefaultGeometry // capacity 18 non-admin

	// Page 0: shows global numbers 1..18, the page-1 indicator, and NOT item 19.
	p0, mapping, _ := MenuScreen(g, svcs, false, false, MenuStatus{}, "", 0)
	if len(mapping) != 22 {
		t.Fatalf("mapping = %d, want 22 (full list)", len(mapping))
	}
	if mapping["19"].Name != "SVC19" {
		t.Errorf(`mapping["19"] = %q, want SVC19 (off-page but selectable)`, mapping["19"].Name)
	}
	if !hasContent(p0, fmt.Sprintf("%3d", 1)) {
		t.Errorf("page 0 missing global number 1")
	}
	if hasContent(p0, fmt.Sprintf("%3d", 19)) {
		t.Errorf("page 0 should not render global number 19")
	}
	if !hasContent(p0, "ITEMS 1 TO 18 OF 22") {
		t.Errorf("page 0 missing indicator 'ITEMS 1 TO 18 OF 22'")
	}

	// Page 1: shows global numbers 19..22, the page-2 indicator, and NOT item 1.
	p1, _, _ := MenuScreen(g, svcs, false, false, MenuStatus{}, "", 1)
	if !hasContent(p1, fmt.Sprintf("%3d", 19)) {
		t.Errorf("page 1 missing global number 19")
	}
	if hasContent(p1, fmt.Sprintf("%3d", 1)) {
		t.Errorf("page 1 should not render global number 1")
	}
	if !hasContent(p1, "ITEMS 19 TO 22 OF 22") {
		t.Errorf("page 1 missing indicator 'ITEMS 19 TO 22 OF 22'")
	}
}

// GH #130/#133: "0 Settings" leads the list on page 0 only; "A Administration"
// flows one blank row below the page's service rows on every page.
func TestMenuScreenMetaBandOnEveryPage(t *testing.T) {
	svcs := make([]store.Service, 22)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i+1), Host: "h", Port: 23}
	}
	g := DefaultGeometry // admin capacity 17: page 0 renders 17 services, page 1 renders 5
	for _, c := range []struct {
		page         int
		svcRows      int
		wantSettings bool
	}{
		{0, 17, true},
		{1, 5, false},
	} {
		screen, _, _ := MenuScreen(g, svcs, true, false, MenuStatus{}, "", c.page)
		if got := screenContains(screen, "Settings"); got != c.wantSettings {
			t.Errorf("page %d: Settings present = %v, want %v", c.page, got, c.wantSettings)
		}
		if !screenContains(screen, "Administration") {
			t.Errorf("page %d missing 'A Administration' meta entry", c.page)
		}
		// First service row is BodyTopRow(), shifted down by one on page 0 by the
		// leading "0 Settings" row. "A" flows one blank separator row below the
		// last service row.
		firstSvc := g.BodyTopRow()
		if c.wantSettings {
			firstSvc++ // the settings row occupies BodyTopRow()
		}
		lastSvc := firstSvc + c.svcRows - 1
		admin, _ := fieldByContent(screen, "System Administration")
		if admin.Row != lastSvc+2 {
			t.Errorf("page %d: Administration row = %d, want %d", c.page, admin.Row, lastSvc+2)
		}
	}
}

func TestMenuScreenSeparatorBetweenListAndBand(t *testing.T) {
	svcs := make([]store.Service, 22)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i+1), Host: "h", Port: 23}
	}
	g := DefaultGeometry
	screen, _, _ := MenuScreen(g, svcs, true, false, MenuStatus{}, "", 0)
	// Full admin page 0: "0 Settings" (row 3) + 17 services (rows 4..20), so the
	// blank separator is row 21 and "A" rides BodyBottomRow (22). Row 21 is below
	// the right-hand status block (rows 3..8), so the whole row is blank.
	sep := g.BodyTopRow() + g.MenuCapacity(true) + 1
	for _, f := range screen {
		if f.Row == sep && f.Content != "" {
			t.Errorf("separator row %d should be blank, found %+v", sep, f)
		}
	}
}

// GH #130: the "0 Settings" option moves to the TOP of the service list on the
// first page (immediately above service 1), rendered as a full tri-color grid
// row — number "  0" / "Settings" name (col 6) / "User and security parameters"
// description (col 17) — matching the ISPF Primary Option Menu's "0  Settings"
// convention. The old bottom-band "User Settings" label is gone.
func TestMenuSettingsRowAtTopOfFirstPage(t *testing.T) {
	g := DefaultGeometry
	svcs := []store.Service{
		{ID: 1, Name: "PROD", Description: "Production CICS", Host: "h", Port: 23},
		{ID: 2, Name: "TEST", Description: "Test CICS", Host: "h", Port: 23},
	}
	screen, _, _ := MenuScreen(g, svcs, false, false, MenuStatus{}, "", 0)

	top := g.BodyTopRow() // first body row, above service 1

	num, ok := fieldByContent(screen, "  0")
	if !ok || num.Row != top || num.Col != 0 {
		t.Errorf(`"  0" number = row %d col %d ok=%v, want row %d col 0`, num.Row, num.Col, ok, top)
	}
	name, ok := fieldByContent(screen, "Settings")
	if !ok || name.Row != top || name.Col != 6 || name.Color != go3270.Turquoise {
		t.Errorf(`"Settings" name = row %d col %d color %v ok=%v, want row %d col 6 turquoise`, name.Row, name.Col, name.Color, ok, top)
	}
	desc, ok := fieldByContent(screen, "User and security parameters")
	if !ok || desc.Row != top || desc.Col != 17 || desc.Color != go3270.Green {
		t.Errorf(`settings description = row %d col %d color %v ok=%v, want row %d col 17 green`, desc.Row, desc.Col, desc.Color, ok, top)
	}
	// Service 1 now renders directly below the settings row.
	svc1, ok := fieldByContent(screen, "PROD")
	if !ok || svc1.Row != top+1 {
		t.Errorf("service 1 row = %d ok=%v, want %d (below the settings row)", svc1.Row, ok, top+1)
	}
	// The old bottom-band "User Settings" label is gone.
	if screenContains(screen, "User Settings") {
		t.Error(`obsolete "User Settings" label still present`)
	}
}

// GH #130: the top "0 Settings" row appears only on the first page; later pages
// start their service window at the first body row with no settings row above.
func TestMenuSettingsRowOnlyOnFirstPage(t *testing.T) {
	g := DefaultGeometry
	svcs := make([]store.Service, g.MenuCapacity(false)+3) // forces a 2nd page
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i+1), Host: "h", Port: 23}
	}
	screen, _, _ := MenuScreen(g, svcs, false, false, MenuStatus{}, "", 1)
	if screenContains(screen, "Settings") {
		t.Error("page 1 must not show the top Settings row")
	}
	// First service of page 1 sits at the first body row (no settings row above).
	first := fmt.Sprintf("SVC%02d", g.MenuCapacity(false)+1)
	f, ok := fieldByContent(screen, first)
	if !ok || f.Row != g.BodyTopRow() {
		t.Errorf("page 1 first service %q row = %d ok=%v, want %d", first, f.Row, ok, g.BodyTopRow())
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
	// non-admin, locked: neither "0 Settings" nor "Administration".
	screen, _, _ := MenuScreen(DefaultGeometry, nil, false, true, MenuStatus{}, "", 0)
	if screenContains(screen, "Settings") {
		t.Error("locked non-admin must not see Settings entry")
	}
	if screenContains(screen, "Administration") {
		t.Error("non-admin must never see Administration")
	}

	// non-admin, unlocked: Settings present (regression).
	screen, _, _ = MenuScreen(DefaultGeometry, nil, false, false, MenuStatus{}, "", 0)
	if !screenContains(screen, "Settings") {
		t.Error("unlocked non-admin must see Settings entry")
	}

	// admin, locked: Administration present, Settings hidden.
	screen, _, _ = MenuScreen(DefaultGeometry, nil, true, true, MenuStatus{}, "", 0)
	if screenContains(screen, "Settings") {
		t.Error("locked admin must not see Settings entry")
	}
	if !screenContains(screen, "Administration") {
		t.Error("locked admin must still see Administration")
	}

	// admin, unlocked: both present.
	screen, _, _ = MenuScreen(DefaultGeometry, nil, true, false, MenuStatus{}, "", 0)
	if !screenContains(screen, "Settings") || !screenContains(screen, "Administration") {
		t.Error("unlocked admin must see both entries")
	}
}

// TestMenuScreenLockedHidesSettingsAcrossPages guards that a settings-locked
// user never sees the "0 Settings" option — including on page 0, where it would
// otherwise lead the list. (On later pages it is page-0-only for everyone; see
// TestMenuSettingsRowOnlyOnFirstPage.)
func TestMenuScreenLockedHidesSettingsAcrossPages(t *testing.T) {
	g := DefaultGeometry
	svcs := make([]store.Service, 2*g.MenuCapacity(false)+1)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i), Host: "h", Port: 23}
	}
	for _, page := range []int{0, 1} {
		screen, _, _ := MenuScreen(g, svcs, false, true, MenuStatus{}, "", page)
		if screenContains(screen, "Settings") {
			t.Errorf("locked user must not see Settings entry on page %d", page)
		}
	}
	// page 0, unlocked: Settings present (regression).
	screen, _, _ := MenuScreen(g, svcs, false, false, MenuStatus{}, "", 0)
	if !screenContains(screen, "Settings") {
		t.Error("unlocked user must see Settings entry on page 0")
	}
}

// GH #130/#133: "0 Settings" leads the list (page 0); "A Administration" flows
// directly below the service list with one blank separator row.
func TestMenuScreenMetaBandFlowsBelowSparseList(t *testing.T) {
	g := DefaultGeometry
	svcs := []store.Service{
		{ID: 1, Name: "PROD", Description: "Production CICS", Host: "h", Port: 23},
		{ID: 2, Name: "TEST", Description: "Test CICS", Host: "h", Port: 23},
		{ID: 3, Name: "DEMO", Description: "Demo backend", Host: "h", Port: 23},
	}
	settingsRow := g.BodyTopRow() // "0 Settings" leads the list
	lastSvc := settingsRow + 3    // 3 services directly below the settings row

	// admin, unlocked: settings leads at the top; "A" one blank row below the list.
	screen, _, _ := MenuScreen(g, svcs, true, false, MenuStatus{}, "", 0)
	s, ok := fieldByContent(screen, "Settings")
	if !ok || s.Row != settingsRow {
		t.Errorf("admin: Settings row = %d ok=%v, want %d", s.Row, ok, settingsRow)
	}
	adm, ok := fieldByContent(screen, "System Administration")
	if !ok || adm.Row != lastSvc+2 {
		t.Errorf("admin: Administration row = %d ok=%v, want %d", adm.Row, ok, lastSvc+2)
	}

	// non-admin, unlocked: settings still leads; no "A".
	screen, _, _ = MenuScreen(g, svcs, false, false, MenuStatus{}, "", 0)
	s, ok = fieldByContent(screen, "Settings")
	if !ok || s.Row != settingsRow {
		t.Errorf("non-admin: Settings row = %d ok=%v, want %d", s.Row, ok, settingsRow)
	}
	if screenContains(screen, "Administration") {
		t.Error("non-admin must not see Administration")
	}
}

// GH #130/#133: a settings-locked admin has no "0 Settings" row, so the service
// list starts at the first body row and "A" flows one blank row below it.
func TestMenuScreenLockedAdminBandTakesAnchorRow(t *testing.T) {
	g := DefaultGeometry
	svcs := []store.Service{{ID: 1, Name: "PROD", Description: "Production CICS", Host: "h", Port: 23}}
	lastSvc := g.BodyTopRow() // single service at the first body row (no settings row)
	screen, _, _ := MenuScreen(g, svcs, true, true, MenuStatus{}, "", 0)
	if screenContains(screen, "Settings") {
		t.Error("locked admin must not see Settings entry")
	}
	adm, ok := fieldByContent(screen, "System Administration")
	if !ok || adm.Row != lastSvc+2 {
		t.Errorf("locked admin: Administration row = %d ok=%v, want %d", adm.Row, ok, lastSvc+2)
	}
}

// GH #130/#133: with an empty list and the leading "0 Settings" row, the
// placeholder sits directly below settings and "A" hugs the placeholder (no
// blank separator — the placeholder is a message, not a service row).
func TestMenuScreenMetaBandHugsEmptyPlaceholder(t *testing.T) {
	g := DefaultGeometry
	screen, _, _ := MenuScreen(g, nil, true, false, MenuStatus{}, "", 0)
	s, ok := fieldByContent(screen, "Settings")
	if !ok || s.Row != g.BodyTopRow() {
		t.Fatalf("Settings row = %d ok=%v, want %d", s.Row, ok, g.BodyTopRow())
	}
	ph, ok := fieldByContent(screen, "(no services available for your account)")
	if !ok || ph.Row != s.Row+1 {
		t.Fatalf("placeholder row = %d ok=%v, want %d (below settings)", ph.Row, ok, s.Row+1)
	}
	adm, ok := fieldByContent(screen, "System Administration")
	if !ok || adm.Row != ph.Row+1 {
		t.Errorf("Administration row = %d ok=%v, want %d (hugs placeholder)", adm.Row, ok, ph.Row+1)
	}
}

// GH #130/#133: a sparse final page has no "0 Settings" row (page-0-only); for
// an admin, "A" flows one blank row below the short final-page list.
func TestMenuScreenMetaBandFlowsOnSparseFinalPage(t *testing.T) {
	g := DefaultGeometry
	svcs := make([]store.Service, g.MenuCapacity(true)+5) // admin page 1 renders 5
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i+1), Host: "h", Port: 23}
	}
	screen, _, _ := MenuScreen(g, svcs, true, false, MenuStatus{}, "", 1)
	if screenContains(screen, "Settings") {
		t.Error("final page must not show the Settings row")
	}
	lastSvc := g.BodyTopRow() + 4 // 5 services starting at the first body row (rows 3..7)
	adm, ok := fieldByContent(screen, "System Administration")
	if !ok || adm.Row != lastSvc+2 {
		t.Errorf("page 1: Administration row = %d ok=%v, want %d", adm.Row, ok, lastSvc+2)
	}
}

// GH #133: locked non-admin with services — the band is empty, so nothing
// renders in the band columns below the last service row (the right-hand
// status block and the PF legend legitimately occupy other columns/rows).
func TestMenuScreenLockedNonAdminNoBandWithServices(t *testing.T) {
	g := DefaultGeometry
	svcs := []store.Service{
		{ID: 1, Name: "PROD", Description: "Production CICS", Host: "h", Port: 23},
		{ID: 2, Name: "TEST", Description: "Test CICS", Host: "h", Port: 23},
		{ID: 3, Name: "DEMO", Description: "Demo backend", Host: "h", Port: 23},
	}
	lastSvc := g.BodyTopRow() + 3 // service rows 4..6 on MOD 2
	screen, _, _ := MenuScreen(g, svcs, false, true, MenuStatus{}, "", 0)
	for _, f := range screen {
		if f.Row > lastSvc && f.Row <= g.BodyBottomRow() && (f.Col == 0 || f.Col == 17) && f.Content != "" {
			t.Errorf("locked non-admin: unexpected band-column field below service list: %+v", f)
		}
	}
}
