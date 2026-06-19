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
//
// The four detail labels are dot-leader aligned (each exactly 21 runes, colon at
// the last column) so their colons line up; their values/input sit one column
// past the input attribute byte at col 25 (content col 26).
func EnrollMFAScreen(geom Geometry, issuer, account, chunkedSecret, errMsg string) (go3270.Screen, go3270.Rules, Cursor) {
	title := "TN3270 GATEWAY: MFA ENROLLMENT"
	const labelCol, valueCol = 2, 25 // label attribute / value (and input) attribute
	code := go3270.Field{Row: 11, Col: valueCol, Name: FieldMFACode, Write: true, NumericOnly: true, Color: go3270.Green, Highlighting: go3270.Underscore}
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len(title)), Color: go3270.White, Intense: true, Content: title},
		{Row: 3, Col: labelCol, Color: go3270.Yellow, Intense: true, Content: "Multi-factor authentication is now required for your account."},
		{Row: 5, Col: labelCol, Color: go3270.Turquoise, Content: "Enter the key below into your authenticator app (any TOTP app),"},
		{Row: 6, Col: labelCol, Color: go3270.Turquoise, Content: "then type the current 6-digit code to confirm enrollment."},
		// Detail rows: turquoise dot-leader label + value; the Key value is white
		// intense to draw the eye to the bit the user must copy.
		{Row: 8, Col: labelCol, Color: go3270.Turquoise, Content: "Issuer  . . . . . . :"},
		{Row: 8, Col: valueCol, Color: go3270.Turquoise, Content: issuer},
		{Row: 9, Col: labelCol, Color: go3270.Turquoise, Content: "Account . . . . . . :"},
		{Row: 9, Col: valueCol, Color: go3270.Turquoise, Content: account},
		{Row: 10, Col: labelCol, Color: go3270.Turquoise, Content: "Key . . . . . . . . :"},
		{Row: 10, Col: valueCol, Color: go3270.White, Intense: true, Content: chunkedSecret},
		{Row: 11, Col: labelCol, Color: go3270.Turquoise, Content: "Confirmation code . :"},
		code,
		{Row: 11, Col: valueCol + 1 + 6}, // stop field (6-digit input)
		{Row: 13, Col: labelCol, Color: go3270.Turquoise, Content: "Save the key in your app before confirming. It won't be shown again."},
		{Row: geom.MessageRow(), Col: labelCol, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		{Row: geom.HelpRow(), Col: 0, Color: go3270.Blue, Content: "Enter=Confirm   PF3=Cancel"},
	}
	rules := go3270.Rules{
		FieldMFACode: {Validator: go3270.NonBlank, ErrorText: "Code is required"},
	}
	return screen, rules, cursorAt(code)
}

// VerifyMFAScreen renders the per-login code prompt for an enrolled user. The
// instruction sits at the body top, a blank row separates it from the dot-leader
// "Code" field (matching the change-password house style), and a lost-device
// recovery hint sits below.
func VerifyMFAScreen(geom Geometry, errMsg string) (go3270.Screen, go3270.Rules, Cursor) {
	title := "TN3270 GATEWAY: MFA VERIFICATION"
	introRow := geom.BodyTopRow() // 3
	codeRow := introRow + 2       // 5 — blank row between instruction and field
	hintRow := codeRow + 3        // 8 — blank rows above the recovery hint
	// "Code . . . :" content at cols 3..14 (colon col 14); input attribute at
	// col 16, 6 digits at cols 17..22, stop field at col 23.
	code := go3270.Field{Row: codeRow, Col: 16, Name: FieldMFACode, Write: true, NumericOnly: true, Color: go3270.Green, Highlighting: go3270.Underscore}
	screen := go3270.Screen{
		{Row: geom.TitleRow(), Col: geom.CenterCol(len(title)), Color: go3270.White, Intense: true, Content: title},
		{Row: introRow, Col: 2, Color: go3270.Turquoise, Content: "Enter the current 6-digit code from your authenticator app."},
		{Row: codeRow, Col: 2, Color: go3270.Turquoise, Content: "Code . . . :"},
		code,
		{Row: codeRow, Col: 23}, // stop field
		{Row: hintRow, Col: 2, Color: go3270.Turquoise, Content: "Lost your device? Contact your administrator to reset MFA."},
		{Row: geom.MessageRow(), Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		{Row: geom.HelpRow(), Col: 0, Color: go3270.Blue, Content: "Enter=Verify   PF3=Cancel"},
	}
	rules := go3270.Rules{
		FieldMFACode: {Validator: go3270.NonBlank, ErrorText: "Code is required"},
	}
	return screen, rules, cursorAt(code)
}
