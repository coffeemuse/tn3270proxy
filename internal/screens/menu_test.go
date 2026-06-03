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
	screen, mapping := MenuScreen(svcs, false, "")

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
	screen, mapping := MenuScreen(nil, false, "")
	if len(mapping) != 0 {
		t.Errorf("mapping should be empty, got %d", len(mapping))
	}
	if _, ok := fieldByName(screen, FieldSelection); !ok {
		t.Errorf("missing %q field", FieldSelection)
	}
}

func TestMenuScreenShowsError(t *testing.T) {
	screen, _ := MenuScreen(nil, false, "Backend unreachable")
	f, ok := fieldByName(screen, FieldError)
	if !ok {
		t.Fatalf("missing error field")
	}
	if f.Content != "Backend unreachable" {
		t.Errorf("error content = %q", f.Content)
	}
}

func TestMenuScreenAdminEntry(t *testing.T) {
	screen, mapping := MenuScreen(nil, true, "")
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

	screen, _ = MenuScreen(nil, false, "")
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
	screen, _ := MenuScreen(svcs, true, "")
	for _, f := range screen {
		if strings.Contains(f.Content, "Administration") && f.Row > 17 {
			t.Errorf("admin entry at row %d would collide with the input line", f.Row)
		}
	}
}
