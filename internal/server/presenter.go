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
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/bridge"
	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/racingmars/go3270"
)

// menuChoice classifies what a menu submit should do.
type menuChoice int

const (
	menuReprompt     menuChoice = iota // invalid key, services available — show inline error
	menuRequery                        // no selectable entries — return nil so session re-queries
	menuAdmin                          // admin "A" entry selected
	menuService                        // valid service key selected
	menuUserSettings                   // "0" user-settings entry selected
	menuQuit                           // PF3 at the menu — logoff
)

// classifyMenuSubmit decides what a menu submit means given the current
// (post-filter) service mapping and whether the admin entry is shown.
// Admin check is evaluated first so "A" with admin=true routes correctly
// even when the service list is empty.
func classifyMenuSubmit(key string, mapping map[string]store.Service, admin bool) (menuChoice, store.Service) {
	if admin && key == "A" {
		return menuAdmin, store.Service{}
	}
	if key == "0" {
		return menuUserSettings, store.Service{}
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

func (go3270Presenter) Login(conn net.Conn, term Term, status screens.MenuStatus, errMsg string) (string, string, bool, error) {
	status.Now = time.Now() // paint-time clock, matching the menu status block
	screen, rules, cur := screens.LoginScreen(term.Geometry(), status, errMsg)
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

func (go3270Presenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, status screens.MenuStatus, errMsg string) (*store.Service, menuChoice, error) {
	geom := term.Geometry()
	status.TermType = term.Type // presenter owns the terminal-derived field
	for {
		status.Now = time.Now() // paint-time clock, refreshed every render
		screen, mapping, cur := screens.MenuScreen(geom, svcs, admin, status, errMsg)
		resp, err := handleScreen(func() (go3270.Response, error) {
			return go3270.HandleScreenAlt(
				screen, nil, map[string]string{},
				[]go3270.AID{go3270.AIDEnter},
				withSilentExits([]go3270.AID{go3270.AIDPF3}),
				screens.FieldError, cur.Row, cur.Col, conn, term.dev, term.codepage(),
			)
		})
		if err != nil {
			return nil, menuReprompt, err // choice ignored on error
		}
		if resp.AID == go3270.AIDPF3 {
			return nil, menuQuit, nil
		}
		key := strings.ToUpper(strings.TrimSpace(resp.Values[screens.FieldSelection]))
		switch choice, svc := classifyMenuSubmit(key, mapping, admin); choice {
		case menuAdmin:
			return nil, menuAdmin, nil
		case menuUserSettings:
			return nil, menuUserSettings, nil
		case menuService:
			return &svc, menuService, nil
		case menuRequery:
			return nil, menuRequery, nil
		default: // menuReprompt
			errMsg = "Invalid selection: " + key
		}
	}
}

func (go3270Presenter) UserSettings(conn net.Conn, term Term, username string, rows []screens.UserSettingsRow, errMsg string) (string, bool, error) {
	geom := term.Geometry()
	valid := make(map[string]bool, len(rows))
	for _, r := range rows {
		valid[r.Key] = true
	}
	for {
		screen, cur := screens.UserSettingsScreen(geom, username, rows, errMsg)
		resp, err := handleScreen(func() (go3270.Response, error) {
			return go3270.HandleScreenAlt(
				screen, nil, map[string]string{},
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
		key := strings.TrimSpace(resp.Values[screens.FieldUSOption])
		if valid[key] {
			return key, false, nil
		}
		errMsg = "Invalid selection: " + key
	}
}

func (go3270Presenter) News(conn net.Conn, term Term, pages [][]string) error {
	geom := term.Geometry()
	for i := 0; i < len(pages); {
		screen, rules, cur := screens.NewsScreen(geom, pages[i])
		resp, err := handleScreen(func() (go3270.Response, error) {
			return go3270.HandleScreenAlt(
				screen, rules, map[string]string{},
				[]go3270.AID{go3270.AIDEnter},
				withSilentExits([]go3270.AID{go3270.AIDPF3}),
				"", cur.Row, cur.Col, conn, term.dev, term.codepage(),
			)
		})
		if err != nil {
			return err
		}
		// ENTER advances (the last page's ENTER ends the loop → nil). PF3
		// returns here but is a deliberate no-op: re-present the same page.
		// PA1/PA2/PA3/Clear never reach here (handleScreen swallows them).
		if resp.AID == go3270.AIDEnter {
			i++
		}
	}
	return nil
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
