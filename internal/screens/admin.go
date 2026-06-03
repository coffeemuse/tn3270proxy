package screens

import (
	"github.com/racingmars/go3270"
)

// Field-name constants for the admin screens.
const (
	FieldOption    = "option" // admin menu option input
	FieldCmdPrefix = "cmd"    // per-row line-command inputs: cmd0, cmd1, ...
	FieldRetype    = "retype" // password confirmation input
	FieldName      = "name"   // entity name input (group/service forms)
	FieldHost      = "host"   // service form inputs
	FieldPort      = "port"   // service form inputs
	FieldTLS       = "tls"    // service form inputs
	FieldVerify    = "verify" // service form inputs
)

// AdminListPageSize is how many data rows fit on an admin list screen
// (rows 4..17 of the fixed 24x80 layout).
const AdminListPageSize = 14

// AdminMenuScreen renders the top-level admin menu. The caller drives it with
// HandleScreen: AIDEnter submits, PF3/PA3 exit (both return to the service
// menu at this level).
func AdminMenuScreen(errMsg string) go3270.Screen {
	return go3270.Screen{
		{Row: 0, Col: 27, Intense: true, Content: "TN3270 GATEWAY ADMIN"},
		{Row: 3, Col: 4, Content: "1.  Users"},
		{Row: 4, Col: 4, Content: "2.  Groups"},
		{Row: 5, Col: 4, Content: "3.  Services"},
		{Row: 19, Col: 2, Content: "===>"},
		{Row: 19, Col: 7, Name: FieldOption, Write: true, Highlighting: go3270.Underscore},
		{Row: 19, Col: 11}, // stop field
		{Row: 21, Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: errMsg},
		{Row: 23, Col: 2, Content: "Enter = select    PF3 / PA3 = main menu"},
	}
}
