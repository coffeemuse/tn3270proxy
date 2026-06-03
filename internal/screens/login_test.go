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
	screen, rules := LoginScreen(DefaultGeometry, "")

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

func TestLoginScreenShowsError(t *testing.T) {
	screen, _ := LoginScreen(DefaultGeometry, "Invalid credentials")
	f, ok := fieldByName(screen, FieldError)
	if !ok {
		t.Fatalf("missing error field")
	}
	if f.Content != "Invalid credentials" {
		t.Errorf("error content = %q, want %q", f.Content, "Invalid credentials")
	}
}

func TestLoginScreenBottomAnchored(t *testing.T) {
	for _, g := range []Geometry{{Rows: 24, Cols: 80}, {Rows: 32, Cols: 80}, {Rows: 43, Cols: 80}, {Rows: 27, Cols: 132}} {
		screen, _ := LoginScreen(g, "err")
		f, ok := fieldByName(screen, FieldError)
		if !ok || f.Row != g.ErrorRow() {
			t.Errorf("%+v: error row = %d, want %d", g, f.Row, g.ErrorRow())
		}
		foundHelp := false
		for _, fl := range screen {
			if fl.Row == g.HelpRow() && fl.Content != "" {
				foundHelp = true
			}
			if fl.Row > g.HelpRow() {
				t.Errorf("%+v: field %+v beyond last row %d", g, fl, g.HelpRow())
			}
		}
		if !foundHelp {
			t.Errorf("%+v: no help line on last row %d", g, g.HelpRow())
		}
	}
}
