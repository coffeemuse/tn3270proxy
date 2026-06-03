package screens

import (
	"testing"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

func TestMenuScreenMapping(t *testing.T) {
	svcs := []store.Service{
		{ID: 1, Name: "PROD CICS", Host: "prod", Port: 23},
		{ID: 2, Name: "TEST CICS", Host: "test", Port: 992, TLS: true},
	}
	screen, mapping := MenuScreen(svcs, "")

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
	screen, mapping := MenuScreen(nil, "")
	if len(mapping) != 0 {
		t.Errorf("mapping should be empty, got %d", len(mapping))
	}
	if _, ok := fieldByName(screen, FieldSelection); !ok {
		t.Errorf("missing %q field", FieldSelection)
	}
}

func TestMenuScreenShowsError(t *testing.T) {
	screen, _ := MenuScreen(nil, "Backend unreachable")
	f, ok := fieldByName(screen, FieldError)
	if !ok {
		t.Fatalf("missing error field")
	}
	if f.Content != "Backend unreachable" {
		t.Errorf("error content = %q", f.Content)
	}
}
