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

import "github.com/racingmars/go3270"

// FieldMFACode is the name of the 6-digit TOTP code input on both MFA screens.
const FieldMFACode = "mfacode"

// EnrollMFAScreen renders the one-time enrollment screen: it shows the issuer,
// account, and the chunked base32 key for manual entry into an authenticator
// app, and prompts for a confirmation code. The caller drives it with
// HandleScreenAlt (AIDEnter submits, AIDPF3 cancels), errorField = FieldError.
func EnrollMFAScreen(geom Geometry, issuer, account, chunkedSecret, errMsg string) (go3270.Screen, go3270.Rules, Cursor) {
	title := "MFA ENROLLMENT - SECURITY KEY SETUP"
	// Row 5 left blank: one row of padding below the yellow notice; the body
	// below it sits a row lower than its natural position.
	code := go3270.Field{Row: 12, Col: 25, Name: FieldMFACode, Write: true, NumericOnly: true, Color: go3270.Green, Highlighting: go3270.Underscore}
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len(title)), Color: go3270.White, Intense: true, Content: title},
		{Row: 4, Col: 2, Color: go3270.Yellow, Intense: true, Content: "Multi-factor authentication is now required for your account."},
		{Row: 6, Col: 2, Color: go3270.Turquoise, Content: "Enter the key below into your authenticator app (any TOTP app),"},
		{Row: 7, Col: 2, Color: go3270.Turquoise, Content: "then type the current 6-digit code to confirm enrollment."},
		{Row: 9, Col: 5, Color: go3270.Turquoise, Content: "Issuer:   " + issuer},
		{Row: 10, Col: 5, Color: go3270.Turquoise, Content: "Account:  " + account},
		{Row: 11, Col: 5, Intense: true, Color: go3270.White, Content: "Key:      " + chunkedSecret},
		{Row: 12, Col: 5, Color: go3270.Turquoise, Content: "Confirmation code:"},
		code,
		{Row: 12, Col: 32}, // stop field
		{Row: geom.MessageRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "Enter=Confirm   PF3=Cancel"},
	}
	rules := go3270.Rules{
		FieldMFACode: {Validator: go3270.NonBlank, ErrorText: "Code is required"},
	}
	return screen, rules, cursorAt(code)
}

// VerifyMFAScreen renders the per-login code prompt for an enrolled user.
func VerifyMFAScreen(geom Geometry, errMsg string) (go3270.Screen, go3270.Rules, Cursor) {
	title := "MFA VERIFICATION"
	code := go3270.Field{Row: 5, Col: 12, Name: FieldMFACode, Write: true, NumericOnly: true, Color: go3270.Green, Highlighting: go3270.Underscore}
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len(title)), Color: go3270.White, Intense: true, Content: title},
		{Row: 4, Col: 2, Color: go3270.Turquoise, Content: "Enter the current 6-digit code from your authenticator app."},
		{Row: 5, Col: 2, Color: go3270.Turquoise, Content: "Code:"},
		code,
		{Row: 5, Col: 19}, // stop field
		{Row: geom.MessageRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		{Row: geom.HelpRow(), Col: 2, Color: go3270.Turquoise, Content: "Enter=Verify   PF3=Cancel"},
	}
	rules := go3270.Rules{
		FieldMFACode: {Validator: go3270.NonBlank, ErrorText: "Code is required"},
	}
	return screen, rules, cursorAt(code)
}
