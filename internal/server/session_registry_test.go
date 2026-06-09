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
	"testing"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/bridge"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

func TestSessionRegistryHelpers_NilRegistryIsNoOp(t *testing.T) {
	s := &Session{} // no Registry
	// Must not panic.
	s.regSetLogin("ALICE")
	s.regClearLogin()
	s.regSetService("PROD")
	s.regClearService()
}

func TestSessionRegistryHelpers_UpdateEntry(t *testing.T) {
	r := newSessionRegistry()
	id := r.register("1.2.3.4:9", time.Unix(100, 0), func() {})
	s := &Session{Registry: r, SessionID: id, Now: func() time.Time { return time.Unix(150, 0) }}

	s.regSetLogin("ALICE")
	if v := r.Snapshot()[0]; v.Username != "ALICE" || !v.LoggedInAt.Equal(time.Unix(150, 0)) {
		t.Fatalf("regSetLogin: %+v", v)
	}
	s.regSetService("PROD")
	if v := r.Snapshot()[0]; v.Service != "PROD" {
		t.Fatalf("regSetService: %q", v.Service)
	}
	s.regClearService()
	s.regClearLogin()
	if v := r.Snapshot()[0]; v.Username != "" || v.Service != "" {
		t.Fatalf("clear helpers: %+v", v)
	}
}

// snapBridger captures the registry snapshot while a session is bridged, so the
// test can observe the mid-bridge state (username + service set).
type snapBridger struct {
	reg    *sessionRegistry
	during []SessionView
	cause  bridge.Cause
}

func (sb *snapBridger) Bridge(conn net.Conn, addr, termType string, escapeAID byte, btls BackendTLS) (bridge.Cause, error) {
	sb.during = sb.reg.Snapshot()
	return sb.cause, nil
}

func TestSessionRun_RegistryTracksLifecycle(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{
			{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}, choice: menuService},
			{quit: true},
		},
	}
	reg := newSessionRegistry()
	sb := &snapBridger{reg: reg, cause: bridge.CauseUserEscaped}
	s := newTestSession(t, p, sb)
	id := reg.register("203.0.113.5:5555", time.Unix(1000, 0), func() {})
	s.Registry = reg
	s.SessionID = id

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(sb.during) != 1 {
		t.Fatalf("mid-bridge snapshot len = %d, want 1", len(sb.during))
	}
	if sb.during[0].Username != "alice" || sb.during[0].Service != "PROD" {
		t.Errorf("mid-bridge view = %+v, want alice/PROD", sb.during[0])
	}
	final := reg.Snapshot()
	if len(final) != 1 {
		t.Fatalf("final snapshot len = %d, want 1 (Run must not deregister)", len(final))
	}
	if final[0].Username != "" || final[0].Service != "" {
		t.Errorf("final view = %+v, want cleared username/service", final[0])
	}
}
