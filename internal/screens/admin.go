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

	"github.com/racingmars/go3270"
)

// Field-name constants for the admin screens.
const (
	FieldOption      = "option"      // admin menu option input
	FieldCmdPrefix   = "cmd"         // per-row line-command inputs: cmd0, cmd1, ...
	FieldRetype      = "retype"      // password confirmation input
	FieldName        = "name"        // entity name input (group/service forms)
	FieldDescription = "description" // service form: human label
	FieldHost        = "host"        // service form inputs
	FieldPort        = "port"        // service form inputs
	FieldTLS         = "tls"         // service form inputs
	FieldVerify      = "verify"      // service form inputs
)

// AdminListView is the view model for an ISPF-style admin list screen: one
// 1-character CMD input per data row plus bottom-anchored legend, error, and
// help lines. The flow layer composes these for users/groups/services.
type AdminListView struct {
	Title   string   // row-0 title
	RowInfo string   // row-0 right side at col 60, e.g. "ROW 1 TO 14 OF 30"
	Header  string   // column header line
	Rows    []string // pre-formatted data rows (CMD inputs added by the builder)
	Legend  string   // line-command legend
	ErrMsg  string   // error / confirm-prompt line
	PFHelp  string   // bottom help line
}

// AdminListScreen renders v sized for geom. Data rows start at row 4; the CMD
// input for row i is named FieldCmdPrefix+i ("cmd0", "cmd1", ...). At most
// geom.ListPageSize() rows fit; rows beyond that are truncated — callers
// paginate via the same method.
func AdminListScreen(geom Geometry, v AdminListView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: 2, Intense: true, Content: v.Title},
		{Row: 0, Col: 60, Content: v.RowInfo},
		{Row: 2, Col: 2, Content: v.Header},
	}
	rows := v.Rows
	if size := geom.ListPageSize(); len(rows) > size {
		rows = rows[:size]
	}
	cur := Cursor{Row: 0, Col: 0} // empty list: no input field, home the cursor
	for i, r := range rows {
		row := 4 + i
		cmd := go3270.Field{Row: row, Col: 2, Name: fmt.Sprintf("%s%d", FieldCmdPrefix, i), Write: true, Highlighting: go3270.Underscore}
		if i == 0 {
			cur = cursorAt(cmd)
		}
		screen = append(screen,
			cmd,
			go3270.Field{Row: row, Col: 4}, // stop field: 1-char command input
			go3270.Field{Row: row, Col: 7, Content: r},
		)
	}
	if len(rows) == 0 {
		screen = append(screen, go3270.Field{Row: 4, Col: 7, Content: "(none)"})
	}
	screen = append(screen,
		go3270.Field{Row: geom.LegendRow(), Col: 2, Content: v.Legend},
		go3270.Field{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Content: v.PFHelp},
	)
	return screen, cur
}

// AdminFormField is one labeled input on an admin form.
type AdminFormField struct {
	Name   string
	Label  string
	Value  string // pre-filled content (edit forms)
	Hidden bool   // non-display (passwords)
	Length int    // input length in columns; effective max 62 (stop field clamps at col 79)
}

// AdminFormView is the view model for a labeled-input admin form screen.
type AdminFormView struct {
	Title  string
	Fields []AdminFormField
	ErrMsg string
}

// AdminFormScreen renders v sized for geom. The first input is at row 3 col 16;
// inputs are two rows apart. The returned Cursor lands on that first input
// (or homes to {0,0} if v has no fields). Fields beyond geom.FormMaxFields()
// are truncated.
func AdminFormScreen(geom Geometry, v AdminFormView) (go3270.Screen, Cursor) {
	screen := go3270.Screen{
		{Row: 0, Col: 2, Intense: true, Content: v.Title},
	}
	fields := v.Fields
	if max := geom.FormMaxFields(); len(fields) > max {
		fields = fields[:max]
	}
	cur := Cursor{Row: 0, Col: 0} // no fields: home the cursor
	for i, f := range fields {
		row := 3 + 2*i
		stopCol := 17 + f.Length
		if stopCol > 79 {
			stopCol = 79
		}
		input := go3270.Field{Row: row, Col: 16, Name: f.Name, Write: true, Hidden: f.Hidden, Content: f.Value, Highlighting: go3270.Underscore}
		if i == 0 {
			cur = cursorAt(input)
		}
		screen = append(screen,
			go3270.Field{Row: row, Col: 2, Content: f.Label},
			input,
			go3270.Field{Row: row, Col: stopCol}, // stop field
		)
	}
	screen = append(screen,
		go3270.Field{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: geom.HelpRow(), Col: 2, Content: "Enter = save    PF3 = cancel"},
	)
	return screen, cur
}

// AdminMenuScreen renders the top-level admin menu sized for geom. The caller
// drives it with HandleScreenAlt: AIDEnter submits, PF3 returns to the
// service menu.
func AdminMenuScreen(geom Geometry, errMsg string) (go3270.Screen, Cursor) {
	option := go3270.Field{Row: geom.InputRow(), Col: 7, Name: FieldOption, Write: true, Highlighting: go3270.Underscore}
	return go3270.Screen{
		{Row: 0, Col: 27, Intense: true, Content: "TN3270 GATEWAY ADMIN"},
		{Row: 3, Col: 4, Content: "1.  Users"},
		{Row: 4, Col: 4, Content: "2.  Groups"},
		{Row: 5, Col: 4, Content: "3.  Services"},
		{Row: geom.InputRow(), Col: 2, Content: "===>"},
		option,
		{Row: geom.InputRow(), Col: 11}, // stop field
		{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		{Row: geom.HelpRow(), Col: 2, Content: "Enter = select    PF3 = main menu"},
	}, cursorAt(option)
}
