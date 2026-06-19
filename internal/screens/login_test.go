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
	"time"

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
	screen, rules, _ := LoginScreen(DefaultGeometry, MenuStatus{}, nil, "")

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
	screen, _, _ := LoginScreen(DefaultGeometry, MenuStatus{}, nil, "Invalid credentials")
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
		screen, _, _ := LoginScreen(g, MenuStatus{}, nil, "err")
		f, ok := fieldByName(screen, FieldError)
		if !ok {
			t.Errorf("%+v: missing error field", g)
		} else if f.Row != 1 {
			t.Errorf("%+v: error row = %d, want row 1", g, f.Row)
		}
		for _, fl := range screen {
			if fl.Row > g.HelpRow() {
				t.Errorf("%+v: field %+v beyond last row %d", g, fl, g.HelpRow())
			}
		}
		help, ok := fieldByContent(screen, "PF3=Disconnect")
		if !ok || help.Row != g.HelpRow() {
			t.Errorf("%+v: PF3=Disconnect help = (found %v) row %d, want row %d", g, ok, help.Row, g.HelpRow())
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
	screen, _, cur := LoginScreen(g, MenuStatus{}, nil, "bad creds")

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
	label, ok := fieldByContent(screen, "User ID . . :")
	if !ok {
		t.Fatal("missing userid label")
	}
	if label.Row != g.BodyBottomRow() || label.Color != go3270.Turquoise {
		t.Errorf("userid label = %+v, want row %d turquoise", label, g.BodyBottomRow())
	}
	user, ok := fieldByName(screen, FieldUsername)
	if !ok {
		t.Fatal("missing username field")
	}
	if user.Color != go3270.Green || !user.Write || user.Row != g.BodyBottomRow() {
		t.Errorf("userid input = %+v, want green writable on row %d", user, g.BodyBottomRow())
	}
	msg, ok := fieldByName(screen, FieldError)
	if !ok {
		t.Fatal("missing error field")
	}
	if msg.Row != 1 || msg.Color != go3270.Red || !msg.Intense {
		t.Errorf("message field = %+v, want row 1 red intense", msg)
	}
	if want := cursorAt(user); cur != want {
		t.Errorf("cursor = %+v, want %+v", cur, want)
	}
}

func TestLoginScreenStatusBlock(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	now := time.Date(2026, 6, 6, 14, 52, 0, 0, time.UTC)
	status := MenuStatus{SystemID: "PROXY", Release: "aa23543", Now: now}
	screen, _, _ := LoginScreen(g, status, nil, "")

	// The info block mirrors the menu's right-hand status block (same column,
	// turquoise labels / green values) but without User ID or Terminal rows.
	want := map[string]string{
		"Date . . :": "26.157", // 2026-06-06 is day 157
		"Time . . :": "14:52",
		"System ID:": "PROXY",
		"Release. :": "aa23543",
	}
	for label, val := range want {
		lf, ok := fieldByContent(screen, label)
		if !ok {
			t.Errorf("missing status label %q", label)
			continue
		}
		if lf.Col != g.StatusBlockCol() || lf.Color != go3270.Turquoise {
			t.Errorf("label %q at col %d color %v, want col %d turquoise", label, lf.Col, lf.Color, g.StatusBlockCol())
		}
		if vf, ok := fieldByContent(screen, val); !ok {
			t.Errorf("missing status value %q for label %q", val, label)
		} else if vf.Color != go3270.Green {
			t.Errorf("value %q color = %v, want Green", val, vf.Color)
		}
	}
	// Pre-login: no User ID and no Terminal rows.
	if _, ok := fieldByContent(screen, "User ID. :"); ok {
		t.Error("login info block must not show a User ID row")
	}
	if _, ok := fieldByContent(screen, "Terminal :"); ok {
		t.Error("login info block must not show a Terminal row")
	}
}

func TestLoginScreenCursor(t *testing.T) {
	screen, _, cur := LoginScreen(DefaultGeometry, MenuStatus{}, nil, "")
	uf, ok := fieldByName(screen, FieldUsername)
	if !ok {
		t.Fatalf("missing %q field", FieldUsername)
	}
	if want := cursorAt(uf); cur != want {
		t.Errorf("login cursor = %+v, want %+v (username field row %d col %d)", cur, want, uf.Row, uf.Col)
	}
}

func fieldAt(s go3270.Screen, row, col int) (go3270.Field, bool) {
	for _, f := range s {
		if f.Row == row && f.Col == col {
			return f, true
		}
	}
	return go3270.Field{}, false
}

func TestLoginScreenCredentialRow(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, _, _ := LoginScreen(g, MenuStatus{}, nil, "")
	row := g.BodyBottomRow()

	pf, ok := fieldByName(screen, FieldPassword)
	if !ok || pf.Row != row {
		t.Fatalf("password field = %+v, want row %d", pf, row)
	}
	if !pf.Hidden {
		t.Errorf("password field must be Hidden")
	}
	// Password input runs from pf.Col+1 to the column before its closing stop
	// field; the requirement is the input reaches column 78 (stop field at 79).
	if _, ok := fieldAt(screen, row, 79); !ok {
		t.Errorf("missing password stop field at col 79 (input must reach col 78)")
	}
	if pf.Col >= 79 {
		t.Errorf("password attribute col = %d, leaves no input before col 79", pf.Col)
	}
	if _, ok := fieldByContent(screen, "Password . . :"); !ok {
		t.Errorf("missing password label")
	}
}

func TestLoginScreenBrandingCentered(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, _, _ := LoginScreen(g, MenuStatus{}, []string{"AAA", "BBB"}, "")
	// 2 lines in an 18-row region (rows 4..21): top padding = (18-2)/2 = 8,
	// so the first line lands on row 4+8 = 12.
	a, ok := fieldByContent(screen, "AAA")
	if !ok || a.Row != 12 || a.Col != 0 {
		t.Errorf("AAA = %+v, want row 12 col 0", a)
	}
	b, ok := fieldByContent(screen, "BBB")
	if !ok || b.Row != 13 {
		t.Errorf("BBB = %+v, want row 13", b)
	}
	if a.Color != go3270.Turquoise {
		t.Errorf("branding color = %v, want Turquoise", a.Color)
	}
}

func TestLoginScreenCopyright(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, _, _ := LoginScreen(g, MenuStatus{}, nil, "")
	c, ok := fieldByContent(screen, loginCopyright)
	if !ok {
		t.Fatalf("copyright line %q not found", loginCopyright)
	}
	if c.Row != g.HelpRow() {
		t.Errorf("copyright row = %d, want HelpRow %d", c.Row, g.HelpRow())
	}
	if c.Color != go3270.White {
		t.Errorf("copyright color = %v, want White", c.Color)
	}
	// Right-aligned: content ends at the screen's right edge (col 79).
	if got := c.Col + len(loginCopyright); got != 79 {
		t.Errorf("copyright ends at col %d, want 79", got)
	}
}

func TestLoginScreenBrandingClipsTopAligned(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80} // region height 18
	lines := make([]string, 25)
	for i := range lines {
		lines[i] = "L" + string(rune('A'+i%26))
	}
	screen, _, _ := LoginScreen(g, MenuStatus{}, lines, "")
	// Overflow: first line top-aligned at LoginBrandingTop (row 4)...
	first, ok := fieldByContent(screen, lines[0])
	if !ok || first.Row != g.LoginBrandingTop() {
		t.Errorf("first branding line = %+v, want row %d", first, g.LoginBrandingTop())
	}
	// ...and nothing renders past the row above the credential row. The
	// PF-key help row legitimately sits at col 0 on HelpRow() (below the
	// credential row), so it is not a branding overrun — exclude it.
	for _, f := range screen {
		if f.Col == 0 && f.Content != "" && f.Row >= g.BodyBottomRow() && f.Row != g.HelpRow() {
			t.Errorf("branding line %q on row %d overruns input row %d", f.Content, f.Row, g.BodyBottomRow())
		}
	}
}

func TestLoginScreenBrandingBlankWhenNil(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	screen, _, _ := LoginScreen(g, MenuStatus{}, nil, "")
	for _, f := range screen {
		if f.Col == 0 && f.Row >= g.LoginBrandingTop() && f.Row < g.BodyBottomRow() && f.Content != "" {
			t.Errorf("unexpected body content with nil branding: %+v", f)
		}
	}
}
