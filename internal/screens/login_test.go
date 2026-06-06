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
	screen, rules, _ := LoginScreen(DefaultGeometry, "")

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
	screen, _, _ := LoginScreen(DefaultGeometry, "Invalid credentials")
	f, ok := fieldByName(screen, FieldError)
	if !ok {
		t.Fatalf("missing error field")
	}
	if f.Content != "Invalid credentials" {
		t.Errorf("error content = %q, want %q", f.Content, "Invalid credentials")
	}
}

func TestLoginScreenBands(t *testing.T) {
	for _, g := range []Geometry{
		{Rows: 24, Cols: 80},
		{Rows: 32, Cols: 80},
		{Rows: 43, Cols: 80},
		{Rows: 27, Cols: 132},
		{}, // zero value normalizes to 24×80
	} {
		screen, _, _ := LoginScreen(g, "err")
		f, ok := fieldByName(screen, FieldError)
		if !ok || f.Row != g.MessageRow() {
			t.Errorf("%+v: error row = %d, want MessageRow %d", g, f.Row, g.MessageRow())
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

func fieldByContent(s go3270.Screen, content string) (go3270.Field, bool) {
	for _, f := range s {
		if f.Content == content {
			return f, true
		}
	}
	return go3270.Field{}, false
}

func TestLoginScreenPalette(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, _, cur := LoginScreen(g, "bad creds")

	title, ok := fieldByContent(screen, "TN3270 GATEWAY LOGIN")
	if !ok {
		t.Fatal("missing title field")
	}
	if title.Row != 0 || !title.Intense || title.Color != go3270.White {
		t.Errorf("title = %+v, want row 0 white intense", title)
	}
	if title.Col != g.CenterCol(len("TN3270 GATEWAY LOGIN")) {
		t.Errorf("title not centered: col %d", title.Col)
	}
	label, ok := fieldByContent(screen, "Userid . . .")
	if !ok {
		t.Fatal("missing userid label")
	}
	if label.Color != go3270.Turquoise {
		t.Errorf("userid label color = %v, want Turquoise", label.Color)
	}
	user, ok := fieldByName(screen, FieldUsername)
	if !ok {
		t.Fatal("missing username field")
	}
	if user.Color != go3270.Green || !user.Write {
		t.Errorf("userid input = %+v, want green writable", user)
	}
	msg, ok := fieldByName(screen, FieldError)
	if !ok {
		t.Fatal("missing error field")
	}
	if msg.Row != 2 || msg.Color != go3270.Red || !msg.Intense {
		t.Errorf("message field = %+v, want row 2 red intense", msg)
	}
	if cur.Row != 3 || cur.Col != 17 {
		t.Errorf("cursor = %+v, want (3,17)", cur)
	}
}

func TestLoginScreenCursor(t *testing.T) {
	screen, _, cur := LoginScreen(DefaultGeometry, "")
	uf, ok := fieldByName(screen, FieldUsername)
	if !ok {
		t.Fatalf("missing %q field", FieldUsername)
	}
	if want := cursorAt(uf); cur != want {
		t.Errorf("login cursor = %+v, want %+v (username field row %d col %d)", cur, want, uf.Row, uf.Col)
	}
}
