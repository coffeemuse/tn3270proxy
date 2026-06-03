package server

import (
	"fmt"
	"net"
	"strings"

	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/racingmars/go3270"
)

// AdminListAction is what the user did on an admin list screen.
type AdminListAction struct {
	Cmd byte // upper-cased line command ('S', 'D', ...), 0 if none
	Row int  // index into the rendered page's rows (valid when Cmd != 0)
	PF  int  // 3 (back), 4 (add), 7/8 (page); 0 for plain Enter
}

// AdminFormAction is what the user did on an admin form screen.
type AdminFormAction struct {
	Values map[string]string // by field name; visible fields trimmed
	Cancel bool              // PF3
}

// AdminPresenter renders the admin screens. The real implementation wraps
// go3270; adminFlow tests use a fake.
type AdminPresenter interface {
	// AdminMenu returns choice 1/2/3 (users/groups/services) or back (PF3,
	// to the service menu). It loops internally on invalid input.
	AdminMenu(conn net.Conn, errMsg string) (choice int, back bool, err error)
	AdminList(conn net.Conn, v screens.AdminListView) (AdminListAction, error)
	AdminForm(conn net.Conn, v screens.AdminFormView) (AdminFormAction, error)
}

var adminListExitKeys = []go3270.AID{
	go3270.AIDPF3, go3270.AIDPF4, go3270.AIDPF7, go3270.AIDPF8,
}

func (go3270Presenter) AdminMenu(conn net.Conn, errMsg string) (int, bool, error) {
	for {
		screen := screens.AdminMenuScreen(screens.DefaultGeometry, errMsg)
		resp, err := go3270.HandleScreen(
			screen, nil, map[string]string{},
			[]go3270.AID{go3270.AIDEnter},
			[]go3270.AID{go3270.AIDPF3},
			screens.FieldError, 19, 8, conn,
		)
		if err != nil {
			return 0, false, err
		}
		if resp.AID == go3270.AIDPF3 {
			return 0, true, nil
		}
		switch strings.TrimSpace(resp.Values[screens.FieldOption]) {
		case "1":
			return 1, false, nil
		case "2":
			return 2, false, nil
		case "3":
			return 3, false, nil
		}
		errMsg = "Invalid option"
	}
}

func (go3270Presenter) AdminList(conn net.Conn, v screens.AdminListView) (AdminListAction, error) {
	screen := screens.AdminListScreen(screens.DefaultGeometry, v)
	// cursor on the first CMD field (attribute col 2 → input col 3); no input
	// fields exist on an empty list, so home the cursor there.
	crow, ccol := 4, 3
	if len(v.Rows) == 0 {
		crow, ccol = 0, 0
	}
	resp, err := go3270.HandleScreen(
		screen, nil, map[string]string{},
		[]go3270.AID{go3270.AIDEnter},
		adminListExitKeys,
		screens.FieldError, crow, ccol, conn,
	)
	if err != nil {
		return AdminListAction{}, err
	}
	return listActionFromResponse(resp, len(v.Rows)), nil
}

func (go3270Presenter) AdminForm(conn net.Conn, v screens.AdminFormView) (AdminFormAction, error) {
	screen := screens.AdminFormScreen(screens.DefaultGeometry, v)
	resp, err := go3270.HandleScreen(
		screen, nil, map[string]string{},
		[]go3270.AID{go3270.AIDEnter},
		[]go3270.AID{go3270.AIDPF3},
		screens.FieldError, 3, 17, conn,
	)
	if err != nil {
		return AdminFormAction{}, err
	}
	return formActionFromResponse(resp, v.Fields), nil
}

// listActionFromResponse maps a HandleScreen response to an AdminListAction.
// The first non-blank CMD field wins (one line command per Enter).
func listActionFromResponse(resp go3270.Response, nRows int) AdminListAction {
	switch resp.AID {
	case go3270.AIDPF3:
		return AdminListAction{PF: 3}
	case go3270.AIDPF4:
		return AdminListAction{PF: 4}
	case go3270.AIDPF7:
		return AdminListAction{PF: 7}
	case go3270.AIDPF8:
		return AdminListAction{PF: 8}
	}
	for i := 0; i < nRows; i++ {
		cmd := strings.ToUpper(strings.TrimSpace(resp.Values[fmt.Sprintf("%s%d", screens.FieldCmdPrefix, i)]))
		if cmd != "" {
			return AdminListAction{Cmd: cmd[0], Row: i}
		}
	}
	return AdminListAction{}
}

// formActionFromResponse maps a HandleScreen response to an AdminFormAction.
// Hidden (password) fields are never trimmed — whitespace may be significant.
func formActionFromResponse(resp go3270.Response, fields []screens.AdminFormField) AdminFormAction {
	if resp.AID == go3270.AIDPF3 {
		return AdminFormAction{Cancel: true}
	}
	vals := make(map[string]string, len(fields))
	for _, f := range fields {
		v := resp.Values[f.Name]
		if !f.Hidden {
			v = strings.TrimSpace(v)
		}
		vals[f.Name] = v
	}
	return AdminFormAction{Values: vals}
}
