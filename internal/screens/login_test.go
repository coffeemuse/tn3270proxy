package screens

import (
	"testing"

	"github.com/racingmars/go3270"
)

func fieldByName(s go3270.Screen, name string) (go3270.Field, bool) {
	for _, f := range s {
		if f.Name == name {
			return f, true
		}
	}
	return go3270.Field{}, false
}

func TestLoginScreenFields(t *testing.T) {
	screen, rules := LoginScreen()

	uf, ok := fieldByName(screen, FieldUsername)
	if !ok {
		t.Fatalf("missing %q field", FieldUsername)
	}
	if !uf.Write {
		t.Errorf("username field should be writable")
	}

	pf, ok := fieldByName(screen, FieldPassword)
	if !ok {
		t.Fatalf("missing %q field", FieldPassword)
	}
	if !pf.Hidden {
		t.Errorf("password field must be Hidden")
	}

	if _, ok := fieldByName(screen, FieldError); !ok {
		t.Errorf("missing %q field for error messages", FieldError)
	}

	// Username must be non-blank to submit.
	if _, ok := rules[FieldUsername]; !ok {
		t.Errorf("expected validation rule on username")
	}
}
