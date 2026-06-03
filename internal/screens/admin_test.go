package screens

import (
	"strings"
	"testing"

	"github.com/racingmars/go3270"
)

func screenContains(s go3270.Screen, sub string) bool {
	for _, f := range s {
		if strings.Contains(f.Content, sub) {
			return true
		}
	}
	return false
}

func TestAdminMenuScreenFields(t *testing.T) {
	screen := AdminMenuScreen("boom")
	f, ok := fieldByName(screen, FieldOption)
	if !ok {
		t.Errorf("missing %q field", FieldOption)
	} else if !f.Write {
		t.Errorf("FieldOption must be writable")
	}
	f, ok = fieldByName(screen, FieldError)
	if !ok || f.Content != "boom" {
		t.Errorf("error field = %+v, ok=%v", f, ok)
	}
	// the three entity options are listed
	for _, want := range []string{"Users", "Groups", "Services"} {
		if !screenContains(screen, want) {
			t.Errorf("menu missing %q", want)
		}
	}
}
