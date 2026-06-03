package screens

import (
	"fmt"
	"strings"
	"testing"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

func TestMenuScreenMapping(t *testing.T) {
	svcs := []store.Service{
		{ID: 1, Name: "PROD CICS", Host: "prod", Port: 23},
		{ID: 2, Name: "TEST CICS", Host: "test", Port: 992, TLS: true},
	}
	screen, mapping := MenuScreen(DefaultGeometry, svcs, false, "")

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
	screen, mapping := MenuScreen(DefaultGeometry, nil, false, "")
	if len(mapping) != 0 {
		t.Errorf("mapping should be empty, got %d", len(mapping))
	}
	if _, ok := fieldByName(screen, FieldSelection); !ok {
		t.Errorf("missing %q field", FieldSelection)
	}
}

func TestMenuScreenShowsError(t *testing.T) {
	screen, _ := MenuScreen(DefaultGeometry, nil, false, "Backend unreachable")
	f, ok := fieldByName(screen, FieldError)
	if !ok {
		t.Fatalf("missing error field")
	}
	if f.Content != "Backend unreachable" {
		t.Errorf("error content = %q", f.Content)
	}
}

func TestMenuScreenAdminEntry(t *testing.T) {
	screen, mapping := MenuScreen(DefaultGeometry, nil, true, "")
	if len(mapping) != 0 {
		t.Errorf("mapping = %v, want empty (admin entry is not a service)", mapping)
	}
	if !screenContains(screen, "A.  Administration") {
		t.Errorf("missing admin entry")
	}
	f, ok := fieldByName(screen, FieldSelection)
	if !ok || f.NumericOnly {
		t.Errorf("selection field must accept 'A' for admins: %+v", f)
	}

	screen, _ = MenuScreen(DefaultGeometry, nil, false, "")
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
	screen, _ := MenuScreen(DefaultGeometry, svcs, true, "")
	for _, f := range screen {
		if strings.Contains(f.Content, "Administration") && f.Row > 17 {
			t.Errorf("admin entry at row %d would collide with the input line", f.Row)
		}
	}
}

func TestMenuScreenHelpSaysLogoff(t *testing.T) {
	screen, _ := MenuScreen(DefaultGeometry, nil, false, "")
	if !screenContains(screen, "PF3 = logoff") {
		t.Errorf("menu help should say PF3 = logoff")
	}
}

func TestMenuScreenBottomAnchored(t *testing.T) {
	for _, g := range []Geometry{{Rows: 24, Cols: 80}, {Rows: 32, Cols: 80}, {Rows: 43, Cols: 80}} {
		screen, _ := MenuScreen(g, nil, false, "err")
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
	screen, mapping := MenuScreen(g2, svcs, false, "")
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
	_, mapping = MenuScreen(g3, svcs, false, "")
	if len(mapping) != g3.MenuCapacity(false) {
		t.Errorf("MOD 3 mapping = %d entries, want %d", len(mapping), g3.MenuCapacity(false))
	}
}

func TestMenuScreenAdminEntryNeverCollidesWhenFull(t *testing.T) {
	svcs := make([]store.Service, 32)
	for i := range svcs {
		svcs[i] = store.Service{ID: int64(i + 1), Name: fmt.Sprintf("SVC%02d", i), Host: "h", Port: 23}
	}
	for _, g := range []Geometry{{Rows: 24, Cols: 80}, {Rows: 43, Cols: 80}} {
		screen, mapping := MenuScreen(g, svcs, true, "")
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
		if occupied[adminRow] != 1 {
			t.Errorf("%+v: admin entry shares row %d with another field", g, adminRow)
		}
	}
}
