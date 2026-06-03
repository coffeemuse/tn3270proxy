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

// LoginScreen returns the login screen and its validation rules. The caller
// drives it with go3270.HandleScreen using AIDEnter to submit and AIDPF3 to
// quit, with errorField = FieldError.
func LoginScreen() (go3270.Screen, go3270.Rules) {
	screen := go3270.Screen{
		{Row: 1, Col: 27, Intense: true, Content: "TN3270 GATEWAY LOGIN"},
		{Row: 4, Col: 2, Content: "Userid . . ."},
		{Row: 4, Col: 16, Name: FieldUsername, Write: true, Highlighting: go3270.Underscore},
		{Row: 4, Col: 33}, // stop field: closes the username input
		{Row: 6, Col: 2, Content: "Password . ."},
		{Row: 6, Col: 16, Name: FieldPassword, Write: true, Hidden: true, Highlighting: go3270.Underscore},
		{Row: 6, Col: 33}, // stop field
		{Row: 22, Col: 2, Content: "Enter = sign on    PF3 = disconnect"},
		{Row: 23, Col: 2, Name: FieldError, Color: go3270.Red, Intense: true},
	}
	rules := go3270.Rules{
		FieldUsername: {Validator: go3270.NonBlank, ErrorText: "Userid is required"},
	}
	return screen, rules
}
