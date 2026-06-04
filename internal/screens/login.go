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

// LoginScreen returns the login screen and its validation rules, sized for
// geom (bottom rows anchored to the last screen rows). errMsg, if non-empty,
// is shown on the error line (e.g. a generic "invalid credentials" message
// after a failed sign-on). The caller drives it with go3270.HandleScreenAlt
// using AIDEnter to submit and AIDPF3 to quit, with errorField = FieldError.
func LoginScreen(geom Geometry, errMsg string) (go3270.Screen, go3270.Rules) {
	screen := go3270.Screen{
		{Row: 0, Col: 27, Intense: true, Content: "TN3270 GATEWAY LOGIN"},
		{Row: 3, Col: 2, Content: "Userid . . ."},
		{Row: 3, Col: 16, Name: FieldUsername, Write: true, Highlighting: go3270.Underscore},
		{Row: 3, Col: 33}, // stop field: closes the username input
		{Row: 5, Col: 2, Content: "Password . ."},
		{Row: 5, Col: 16, Name: FieldPassword, Write: true, Hidden: true, Highlighting: go3270.Underscore},
		{Row: 5, Col: 33}, // stop field
		{Row: geom.ErrorRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		{Row: geom.HelpRow(), Col: 2, Content: "Enter = sign on    PF3 = disconnect"},
	}
	rules := go3270.Rules{
		FieldUsername: {Validator: go3270.NonBlank, ErrorText: "Userid is required"},
	}
	return screen, rules
}
