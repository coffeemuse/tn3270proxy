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
	"net"
	"strings"

	"github.com/coffeemuse/tn3270proxy/internal/screens"
	"github.com/racingmars/go3270"
)

// AdminPresenter renders the admin screens. The real implementation wraps
// go3270; adminFlow tests use a fake. term carries the client's negotiated
// screen size (and codepage) from Presenter.Negotiate.
type AdminPresenter interface {
	// AdminMenu returns choice 1-7 (users/groups/services/system params/
	// trusted networks/audit log/active sessions) or back (PF3, to the service
	// menu). It loops internally on invalid input.
	AdminMenu(conn net.Conn, term Term, errMsg string) (choice int, back bool, err error)
}

func (go3270Presenter) AdminMenu(conn net.Conn, term Term, errMsg string) (int, bool, error) {
	geom := term.Geometry()
	for {
		screen, cur := screens.AdminMenuScreen(geom, errMsg)
		resp, err := handleScreen(func() (go3270.Response, error) {
			return go3270.HandleScreenAlt(
				screen, nil, map[string]string{},
				[]go3270.AID{go3270.AIDEnter},
				withSilentExits([]go3270.AID{go3270.AIDPF3}),
				screens.FieldError, cur.Row, cur.Col, conn, term.dev, term.codepage(),
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
		case "4":
			return 4, false, nil
		case "5":
			return 5, false, nil
		case "6":
			return 6, false, nil
		case "7":
			return 7, false, nil
		}
		errMsg = "Invalid option"
	}
}
