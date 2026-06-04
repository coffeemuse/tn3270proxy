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
	"crypto/tls"
	"net"
	"strings"

	"github.com/CoffeeMuse/tn3270proxy/internal/bridge"
	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/racingmars/go3270"
)

// menuChoice classifies what a menu submit should do.
type menuChoice int

const (
	menuReprompt menuChoice = iota // invalid key, services available — show inline error
	menuRequery                    // no selectable entries — return nil so session re-queries
	menuAdmin                      // admin "A" entry selected
	menuService                    // valid service key selected
)

// classifyMenuSubmit decides what a menu submit means given the current
// (post-filter) service mapping and whether the admin entry is shown.
// Admin check is evaluated first so "A" with admin=true routes correctly
// even when the service list is empty.
func classifyMenuSubmit(key string, mapping map[string]store.Service, admin bool) (menuChoice, store.Service) {
	if admin && key == "A" {
		return menuAdmin, store.Service{}
	}
	if svc, ok := mapping[key]; ok {
		return menuService, svc
	}
	if len(mapping) == 0 {
		// Nothing is selectable — re-prompting in place is futile; hand control
		// back to Session.Run so it re-queries the store (honouring "try again").
		return menuRequery, store.Service{}
	}
	return menuReprompt, store.Service{}
}

// go3270Presenter renders screens using the go3270 library over a raw conn.
type go3270Presenter struct{}

func (go3270Presenter) Negotiate(conn net.Conn) (Term, error) {
	dev, err := go3270.NegotiateTelnet(conn)
	if err != nil {
		return Term{}, err
	}
	rows, cols := dev.AltDimensions()
	return normalizeTerm(Term{Type: dev.TerminalType(), Rows: rows, Cols: cols, dev: dev}), nil
}

func (go3270Presenter) Login(conn net.Conn, term Term, errMsg string) (string, string, bool, error) {
	screen, rules, cur := screens.LoginScreen(term.Geometry(), errMsg)
	resp, err := handleScreen(func() (go3270.Response, error) {
		return go3270.HandleScreenAlt(
			screen, rules, map[string]string{},
			[]go3270.AID{go3270.AIDEnter},
			withSilentExits([]go3270.AID{go3270.AIDPF3}),
			screens.FieldError, cur.Row, cur.Col, conn, term.dev, term.codepage(),
		)
	})
	if err != nil {
		return "", "", false, err
	}
	if resp.AID == go3270.AIDPF3 {
		return "", "", true, nil
	}
	return strings.TrimSpace(resp.Values[screens.FieldUsername]),
		resp.Values[screens.FieldPassword], false, nil
}

func (go3270Presenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, errMsg string) (*store.Service, bool, bool, error) {
	geom := term.Geometry()
	for {
		screen, mapping := screens.MenuScreen(geom, svcs, admin, errMsg)
		resp, err := handleScreen(func() (go3270.Response, error) {
			return go3270.HandleScreenAlt(
				screen, nil, map[string]string{},
				[]go3270.AID{go3270.AIDEnter},
				withSilentExits([]go3270.AID{go3270.AIDPF3}),
				screens.FieldError, geom.InputRow(), 8, conn, term.dev, term.codepage(),
			)
		})
		if err != nil {
			return nil, false, false, err
		}
		if resp.AID == go3270.AIDPF3 {
			return nil, false, true, nil
		}
		key := strings.ToUpper(strings.TrimSpace(resp.Values[screens.FieldSelection]))
		switch choice, svc := classifyMenuSubmit(key, mapping, admin); choice {
		case menuAdmin:
			return nil, true, false, nil
		case menuService:
			return &svc, false, false, nil
		case menuRequery:
			return nil, false, false, nil
		default: // menuReprompt
			errMsg = "Invalid selection: " + key
		}
	}
}

// realBridger adapts bridge.Bridge to the Bridger interface.
type realBridger struct{}

func (realBridger) Bridge(client net.Conn, addr, termType string, escapeAID byte, btls BackendTLS) (bridge.Cause, error) {
	return bridge.Bridge(client, addr, termType, escapeAID, backendTLSConfig(addr, btls))
}

// backendTLSConfig builds the dial-time tls.Config for a backend, or nil for a
// plaintext dial. ServerName is always the configured host; verification uses
// the system root store (browser-like). Verify=false encrypts without
// authenticating (for internal hosts with self-signed certs).
func backendTLSConfig(addr string, btls BackendTLS) *tls.Config {
	if !btls.Enabled {
		return nil
	}
	// addr is always net.JoinHostPort output, so SplitHostPort cannot fail;
	// the fallback sets an invalid ServerName that TLS will reject at
	// handshake (loud failure, never a silent verification skip).
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         host,
		InsecureSkipVerify: !btls.Verify,
	}
}
