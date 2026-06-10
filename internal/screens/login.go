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

// Package screens builds the go3270 screens the proxy renders to clients.
package screens

import "github.com/racingmars/go3270"

// Field name constants shared between screen definitions and the session
// code that reads Response.Values.
const (
	FieldUsername = "username"
	FieldPassword = "password"
	FieldError    = "errormsg"
)

// LoginScreen returns the branding-forward login screen, its validation rules,
// and the initial cursor (on the username field), sized for geom. Layout
// (0-based; login is a documented exception to the three-band convention — see
// docs/dev/ispf-style-guide.md):
//
//	row 0     centered title              | Date  (col StatusBlockCol)
//	row 1     error line (col 2)          | Time
//	row 2                                 | System ID
//	row 3                                 | Release
//	rows 4..  branding (cols 0-79 verbatim, vertically centered; first-N
//	  input-1   top-aligned clip when taller than the region)
//	BodyBottomRow  User ID + Password on one line (password input reaches col 78)
//	HelpRow        PF3=Disconnect
//
// branding holds the already-split file lines (see SplitBranding); nil/empty
// renders a blank body. status supplies the right-hand info; the presenter
// stamps status.Now at paint time. errMsg, if non-empty, shows on row 1
// (truncated so it cannot collide with the Time block at StatusBlockCol).
func LoginScreen(geom Geometry, status MenuStatus, branding []string, errMsg string) (go3270.Screen, go3270.Rules, Cursor) {
	title := "TN3270 GATEWAY LOGIN"
	row := geom.BodyBottomRow() // credential row (second-to-last)
	username := go3270.Field{Row: row, Col: 15, Name: FieldUsername, Write: true, Color: go3270.Green, Highlighting: go3270.Underscore}
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len(title)), Color: go3270.White, Intense: true, Content: title},
		// Error on row 1: attribute at col 2, content cols 3..58 (truncated to
		// 56 runes), leaving col 59 free before the Time block at StatusBlockCol
		// (60). Generic errors are far shorter.
		{Row: 1, Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: truncateRunes(errMsg, 56)},
		// Credential row: both fields share one line.
		{Row: row, Col: 2, Color: go3270.Turquoise, Content: "User ID . . :"},
		username,
		{Row: row, Col: 28}, // stop field: closes the username input (cols 16-27)
		{Row: row, Col: 30, Color: go3270.Turquoise, Content: "Password . . :"},
		{Row: row, Col: 44, Name: FieldPassword, Write: true, Hidden: true, Color: go3270.Green, Highlighting: go3270.Underscore},
		{Row: row, Col: 79}, // stop field: password input runs cols 45-78
		{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "PF3=Disconnect"},
	}
	// Status header on rows 0-3 at StatusBlockCol (startRow 0 places the four
	// rows consecutively). No User ID (pre-login) and no Terminal, by design.
	screen = append(screen, statusBlock(geom, 0, []statusRow{
		{"Date . . :", julianDate(status.Now)},
		{"Time . . :", clockHM(status.Now)},
		{"System ID:", truncateRunes(status.SystemID, 7)},
		{"Release. :", truncateRunes(status.Release, 7)},
	})...)
	// Branding region: rows LoginBrandingTop .. (credential row - 1). Vertically
	// centered when it fits; first-N top-aligned clip when taller.
	top, h := geom.LoginBrandingTop(), geom.LoginBrandingHeight()
	lines := branding
	if len(lines) > h {
		lines = lines[:h]
	}
	pad := (h - len(lines)) / 2
	for i, ln := range lines {
		if ln == "" {
			continue // blank line: spacing only, no protected field
		}
		screen = append(screen, go3270.Field{Row: top + pad + i, Col: 0, Color: go3270.White, Content: ln})
	}
	rules := go3270.Rules{
		FieldUsername: {Validator: go3270.NonBlank, ErrorText: "User ID is required"},
	}
	return screen, rules, cursorAt(username)
}
