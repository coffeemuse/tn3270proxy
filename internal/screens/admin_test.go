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
	screen, _ := AdminMenuScreen(DefaultGeometry, "boom")
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

func TestAdminListScreenRowsAndCmdFields(t *testing.T) {
	v := AdminListView{
		Title:   "TN3270 GATEWAY ADMIN: USERS",
		RowInfo: "ROW 1 TO 2 OF 2",
		Header:  "CMD  USERNAME     GROUPS",
		Rows:    []string{"alice  ops", "bob    dev"},
		Legend:  "S = set password",
		ErrMsg:  "oops",
		PFHelp:  "Enter = process",
	}
	screen := AdminListScreen(DefaultGeometry, v)
	for i := range v.Rows {
		name := fmt.Sprintf("%s%d", FieldCmdPrefix, i)
		f, ok := fieldByName(screen, name)
		if !ok {
			t.Fatalf("missing cmd field %q", name)
		}
		if !f.Write {
			t.Errorf("%q not writable", name)
		}
	}
	if _, ok := fieldByName(screen, fmt.Sprintf("%s%d", FieldCmdPrefix, 2)); ok {
		t.Errorf("unexpected extra cmd field")
	}
	for _, want := range []string{"alice  ops", "bob    dev", "ROW 1 TO 2 OF 2", "S = set password"} {
		if !screenContains(screen, want) {
			t.Errorf("screen missing %q", want)
		}
	}
	f, _ := fieldByName(screen, FieldError)
	if f.Content != "oops" {
		t.Errorf("error = %q", f.Content)
	}
}

func TestAdminListScreenEmpty(t *testing.T) {
	screen := AdminListScreen(DefaultGeometry, AdminListView{Title: "T"})
	if _, ok := fieldByName(screen, FieldCmdPrefix+"0"); ok {
		t.Errorf("empty list should have no cmd fields")
	}
	if !screenContains(screen, "(none)") {
		t.Errorf("empty list should say (none)")
	}
}

func TestAdminListScreenTruncatesOverflow(t *testing.T) {
	rows := make([]string, DefaultGeometry.ListPageSize()+3)
	for i := range rows {
		rows[i] = fmt.Sprintf("row%d", i)
	}
	screen := AdminListScreen(DefaultGeometry, AdminListView{Title: "T", Rows: rows})
	last := fmt.Sprintf("%s%d", FieldCmdPrefix, DefaultGeometry.ListPageSize()-1)
	if _, ok := fieldByName(screen, last); !ok {
		t.Errorf("missing last in-page cmd field %q", last)
	}
	if _, ok := fieldByName(screen, fmt.Sprintf("%s%d", FieldCmdPrefix, DefaultGeometry.ListPageSize())); ok {
		t.Errorf("overflow row should be truncated")
	}
}

func TestAdminFormScreenTruncatesOverflow(t *testing.T) {
	fields := make([]AdminFormField, DefaultGeometry.FormMaxFields()+2)
	for i := range fields {
		fields[i] = AdminFormField{Name: fmt.Sprintf("f%d", i), Label: "L", Length: 8}
	}
	screen := AdminFormScreen(DefaultGeometry, AdminFormView{Title: "T", Fields: fields})
	if _, ok := fieldByName(screen, fmt.Sprintf("f%d", DefaultGeometry.FormMaxFields()-1)); !ok {
		t.Errorf("missing last in-form field")
	}
	if _, ok := fieldByName(screen, fmt.Sprintf("f%d", DefaultGeometry.FormMaxFields())); ok {
		t.Errorf("overflow field should be truncated")
	}
}

func TestAdminFormScreenFields(t *testing.T) {
	v := AdminFormView{
		Title: "TN3270 GATEWAY ADMIN: ADD USER",
		Fields: []AdminFormField{
			{Name: FieldUsername, Label: "Userid . . .", Value: "alice", Length: 16},
			{Name: FieldPassword, Label: "Password . .", Hidden: true, Length: 16},
		},
		ErrMsg: "bad",
	}
	screen := AdminFormScreen(DefaultGeometry, v)
	u, ok := fieldByName(screen, FieldUsername)
	if !ok || !u.Write || u.Content != "alice" || u.Hidden {
		t.Errorf("username field = %+v, ok=%v", u, ok)
	}
	p, ok := fieldByName(screen, FieldPassword)
	if !ok || !p.Write || !p.Hidden {
		t.Errorf("password field = %+v, ok=%v", p, ok)
	}
	f, _ := fieldByName(screen, FieldError)
	if f.Content != "bad" {
		t.Errorf("error = %q", f.Content)
	}
	if !screenContains(screen, "PF3 = cancel") {
		t.Errorf("missing cancel help")
	}
}

func TestAdminScreensBottomAnchored(t *testing.T) {
	g := Geometry{Rows: 32, Cols: 80}

	menu, _ := AdminMenuScreen(g, "boom")
	opt, _ := fieldByName(menu, FieldOption)
	if opt.Row != g.InputRow() {
		t.Errorf("admin menu option row = %d, want %d", opt.Row, g.InputRow())
	}

	list := AdminListScreen(g, AdminListView{Title: "T", Legend: "L", ErrMsg: "E", PFHelp: "H", Rows: []string{"r"}})
	e, _ := fieldByName(list, FieldError)
	if e.Row != g.ErrorRow() {
		t.Errorf("list error row = %d, want %d", e.Row, g.ErrorRow())
	}
	legendOK, helpOK := false, false
	for _, f := range list {
		if f.Row == g.LegendRow() && f.Content == "L" {
			legendOK = true
		}
		if f.Row == g.HelpRow() && f.Content == "H" {
			helpOK = true
		}
	}
	if !legendOK || !helpOK {
		t.Errorf("legend on %d / help on %d not found (legendOK=%v helpOK=%v)", g.LegendRow(), g.HelpRow(), legendOK, helpOK)
	}

	form := AdminFormScreen(g, AdminFormView{Title: "T", ErrMsg: "E"})
	e, _ = fieldByName(form, FieldError)
	if e.Row != g.ErrorRow() {
		t.Errorf("form error row = %d, want %d", e.Row, g.ErrorRow())
	}
	formHelpOK := false
	for _, f := range form {
		if f.Row == g.HelpRow() && strings.Contains(f.Content, "PF3 = cancel") {
			formHelpOK = true
		}
	}
	if !formHelpOK {
		t.Errorf("form: no help line on last row %d", g.HelpRow())
	}
}

func TestAdminMenuScreenCursor(t *testing.T) {
	screen, cur := AdminMenuScreen(DefaultGeometry, "")
	of, ok := fieldByName(screen, FieldOption)
	if !ok {
		t.Fatalf("missing %q field", FieldOption)
	}
	if want := cursorAt(of); cur != want {
		t.Errorf("admin menu cursor = %+v, want %+v (option field row %d col %d)", cur, want, of.Row, of.Col)
	}
}

func TestAdminListScreenPageSizeGrowsWithRows(t *testing.T) {
	g := Geometry{Rows: 32, Cols: 80} // page size 22
	rows := make([]string, 25)
	for i := range rows {
		rows[i] = fmt.Sprintf("row%d", i)
	}
	screen := AdminListScreen(g, AdminListView{Title: "T", Rows: rows})
	last := fmt.Sprintf("%s%d", FieldCmdPrefix, g.ListPageSize()-1)
	if _, ok := fieldByName(screen, last); !ok {
		t.Errorf("missing last in-page cmd field %q", last)
	}
	if _, ok := fieldByName(screen, fmt.Sprintf("%s%d", FieldCmdPrefix, g.ListPageSize())); ok {
		t.Errorf("row beyond MOD 3 page size should be truncated")
	}
}
