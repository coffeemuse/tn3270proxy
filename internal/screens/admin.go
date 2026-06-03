package screens

import (
	"fmt"

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

// AdminListScreen renders v. Data rows start at row 4; the CMD input for row i
// is named FieldCmdPrefix+i ("cmd0", "cmd1", ...). At most AdminListPageSize
// rows fit; rows beyond AdminListPageSize are truncated — callers paginate via
// AdminListPageSize.
func AdminListScreen(v AdminListView) go3270.Screen {
	screen := go3270.Screen{
		{Row: 0, Col: 2, Intense: true, Content: v.Title},
		{Row: 0, Col: 60, Content: v.RowInfo},
		{Row: 2, Col: 2, Content: v.Header},
	}
	rows := v.Rows
	if len(rows) > AdminListPageSize {
		rows = rows[:AdminListPageSize]
	}
	for i, r := range rows {
		row := 4 + i
		screen = append(screen,
			go3270.Field{Row: row, Col: 2, Name: fmt.Sprintf("%s%d", FieldCmdPrefix, i), Write: true, Highlighting: go3270.Underscore},
			go3270.Field{Row: row, Col: 4}, // stop field: 1-char command input
			go3270.Field{Row: row, Col: 7, Content: r},
		)
	}
	if len(rows) == 0 {
		screen = append(screen, go3270.Field{Row: 4, Col: 7, Content: "(none)"})
	}
	screen = append(screen,
		go3270.Field{Row: 20, Col: 2, Content: v.Legend},
		go3270.Field{Row: 21, Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: 23, Col: 2, Content: v.PFHelp},
	)
	return screen
}

// AdminFormField is one labeled input on an admin form.
type AdminFormField struct {
	Name   string
	Label  string
	Value  string // pre-filled content (edit forms)
	Hidden bool   // non-display (passwords)
	Length int    // input length in columns
}

// AdminFormView is the view model for a labeled-input admin form screen.
type AdminFormView struct {
	Title  string
	Fields []AdminFormField
	ErrMsg string
}

// AdminFormScreen renders v. The first input is at row 3 col 16 (so the
// caller's initial cursor is (3, 17)); inputs are two rows apart.
func AdminFormScreen(v AdminFormView) go3270.Screen {
	screen := go3270.Screen{
		{Row: 0, Col: 2, Intense: true, Content: v.Title},
	}
	for i, f := range v.Fields {
		row := 3 + 2*i
		screen = append(screen,
			go3270.Field{Row: row, Col: 2, Content: f.Label},
			go3270.Field{Row: row, Col: 16, Name: f.Name, Write: true, Hidden: f.Hidden, Content: f.Value, Highlighting: go3270.Underscore},
			go3270.Field{Row: row, Col: 17 + f.Length}, // stop field
		)
	}
	screen = append(screen,
		go3270.Field{Row: 21, Col: 2, Name: FieldError, Color: go3270.Red, Intense: true, Content: v.ErrMsg},
		go3270.Field{Row: 23, Col: 2, Content: "Enter = save    PF3 = cancel    PA3 = main menu"},
	)
	return screen
}

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
