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

func (go3270Presenter) EnrollMFA(conn net.Conn, term Term, issuer, account, chunkedSecret, errMsg string) (string, bool, error) {
	screen, rules, cur := screens.EnrollMFAScreen(term.Geometry(), issuer, account, chunkedSecret, errMsg)
	resp, err := handleScreen(func() (go3270.Response, error) {
		return go3270.HandleScreenAlt(
			screen, rules, map[string]string{},
			[]go3270.AID{go3270.AIDEnter},
			withSilentExits([]go3270.AID{go3270.AIDPF3}),
			screens.FieldError, cur.Row, cur.Col, conn, term.dev, term.codepage(),
		)
	})
	if err != nil {
		return "", false, err
	}
	if resp.AID == go3270.AIDPF3 {
		return "", true, nil
	}
	return strings.TrimSpace(resp.Values[screens.FieldMFACode]), false, nil
}

func (go3270Presenter) VerifyMFA(conn net.Conn, term Term, errMsg string) (string, bool, error) {
	screen, rules, cur := screens.VerifyMFAScreen(term.Geometry(), errMsg)
	resp, err := handleScreen(func() (go3270.Response, error) {
		return go3270.HandleScreenAlt(
			screen, rules, map[string]string{},
			[]go3270.AID{go3270.AIDEnter},
			withSilentExits([]go3270.AID{go3270.AIDPF3}),
			screens.FieldError, cur.Row, cur.Col, conn, term.dev, term.codepage(),
		)
	})
	if err != nil {
		return "", false, err
	}
	if resp.AID == go3270.AIDPF3 {
		return "", true, nil
	}
	return strings.TrimSpace(resp.Values[screens.FieldMFACode]), false, nil
}
