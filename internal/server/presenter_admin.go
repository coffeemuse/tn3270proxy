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
// go3270; adminFlow tests use a fake. term carries the client's negotiated
// screen size (and codepage) from Presenter.Negotiate.
type AdminPresenter interface {
	// AdminMenu returns choice 1/2/3 (users/groups/services) or back (PF3,
	// to the service menu). It loops internally on invalid input.
	AdminMenu(conn net.Conn, term Term, errMsg string) (choice int, back bool, err error)
	AdminList(conn net.Conn, term Term, v screens.AdminListView) (AdminListAction, error)
	AdminForm(conn net.Conn, term Term, v screens.AdminFormView) (AdminFormAction, error)
}

var adminListExitKeys = []go3270.AID{
	go3270.AIDPF3, go3270.AIDPF4, go3270.AIDPF7, go3270.AIDPF8,
}

func (go3270Presenter) AdminMenu(conn net.Conn, term Term, errMsg string) (int, bool, error) {
	geom := term.Geometry()
	for {
		screen := screens.AdminMenuScreen(geom, errMsg)
		resp, err := handleScreen(func() (go3270.Response, error) {
			return go3270.HandleScreenAlt(
				screen, nil, map[string]string{},
				[]go3270.AID{go3270.AIDEnter},
				withSilentExits([]go3270.AID{go3270.AIDPF3}),
				screens.FieldError, geom.InputRow(), 8, conn, term.dev, term.codepage(),
			)
		})
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

func (go3270Presenter) AdminList(conn net.Conn, term Term, v screens.AdminListView) (AdminListAction, error) {
	screen := screens.AdminListScreen(term.Geometry(), v)
	// cursor on the first CMD field (attribute col 2 → input col 3); no input
	// fields exist on an empty list, so home the cursor there.
	crow, ccol := 4, 3
	if len(v.Rows) == 0 {
		crow, ccol = 0, 0
	}
	resp, err := handleScreen(func() (go3270.Response, error) {
		return go3270.HandleScreenAlt(
			screen, nil, map[string]string{},
			[]go3270.AID{go3270.AIDEnter},
			withSilentExits(adminListExitKeys),
			screens.FieldError, crow, ccol, conn, term.dev, term.codepage(),
		)
	})
	if err != nil {
		return AdminListAction{}, err
	}
	return listActionFromResponse(resp, len(v.Rows)), nil
}

func (go3270Presenter) AdminForm(conn net.Conn, term Term, v screens.AdminFormView) (AdminFormAction, error) {
	screen := screens.AdminFormScreen(term.Geometry(), v)
	resp, err := handleScreen(func() (go3270.Response, error) {
		return go3270.HandleScreenAlt(
			screen, nil, map[string]string{},
			[]go3270.AID{go3270.AIDEnter},
			withSilentExits([]go3270.AID{go3270.AIDPF3}),
			screens.FieldError, 3, 17, conn, term.dev, term.codepage(),
		)
	})
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
