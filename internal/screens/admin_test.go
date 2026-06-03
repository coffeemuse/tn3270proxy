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
	screen := AdminMenuScreen("boom")
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
	screen := AdminListScreen(v)
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
	screen := AdminListScreen(AdminListView{Title: "T"})
	if _, ok := fieldByName(screen, FieldCmdPrefix+"0"); ok {
		t.Errorf("empty list should have no cmd fields")
	}
	if !screenContains(screen, "(none)") {
		t.Errorf("empty list should say (none)")
	}
}
