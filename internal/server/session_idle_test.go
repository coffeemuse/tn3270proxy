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
	"os"
	"slices"
	"testing"
	"time"

	"github.com/coffeemuse/tn3270proxy/internal/bridge"
	"github.com/coffeemuse/tn3270proxy/internal/store"
)

// idleRecordingConn stands in for the idleConn wrapper, recording the regime
// transitions the session drives.
type idleRecordingConn struct {
	net.Conn
	calls []string
}

func (c *idleRecordingConn) setPreAuth(idle, max time.Duration) {
	c.calls = append(c.calls, "preauth:"+idle.String()+"/"+max.String())
}
func (c *idleRecordingConn) setWindow(idle time.Duration) {
	c.calls = append(c.calls, "window:"+idle.String())
}

func TestSessionRegimeTransitions(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true},
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.PreAuthIdle = 2 * time.Minute
	s.Idle = 30 * time.Minute
	s.PreAuthMax = 5 * time.Minute

	pipe, _ := net.Pipe()
	defer pipe.Close()
	client := &idleRecordingConn{Conn: pipe}
	s.Run(client)

	want := []string{
		"preauth:2m0s/5m0s", // connect
		"window:30m0s",      // auth ok → post-auth
		"preauth:2m0s/5m0s", // PF3 logoff → back to pre-auth
	}
	if !slices.Equal(client.calls, want) {
		t.Errorf("regime calls = %v, want %v", client.calls, want)
	}
}

func TestSessionZeroIdleConfigLeavesConnAlone(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{}) // PreAuthIdle/Idle/PreAuthMax left zero

	pipe, _ := net.Pipe()
	defer pipe.Close()
	client := &idleRecordingConn{Conn: pipe}
	s.Run(client)

	if len(client.calls) != 0 {
		t.Errorf("regime calls = %v, want none when idle config is zero", client.calls)
	}
}

func TestSessionAuditsIdleTimeoutAtLogin(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{err: os.ErrDeadlineExceeded}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	disc := rec.events[len(rec.events)-1]
	if disc.Kind != store.AuditDisconnect || disc.Detail != "idle timeout" {
		t.Errorf("disconnect = %+v, want Detail %q", disc, "idle timeout")
	}
}

func TestSessionIdleAtMenuLogsOutToLogin(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // re-presented login after idle-logout
		},
		menuPicks: []menuResult{{err: os.ErrDeadlineExceeded}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.logins) != 0 {
		t.Fatalf("expected both login renders consumed, %d left (login not re-presented)", len(p.logins))
	}
	var logout *store.AuditEvent
	for i := range rec.events {
		if rec.events[i].Kind == store.AuditLogout {
			logout = &rec.events[i]
		}
	}
	if logout == nil || logout.Detail != "idle logout" || logout.Username != "alice" {
		t.Errorf("want AuditLogout{idle logout, alice}, got %+v", logout)
	}
}

func TestSessionPF3LogoffAuditsLogout(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	rec := &recordingAuditor{}
	s.Auditor = rec
	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	found := false
	for _, e := range rec.events {
		if e.Kind == store.AuditLogout && e.Detail == "user logoff" {
			found = true
		}
	}
	if !found {
		t.Errorf("PF3 logoff did not emit AuditLogout{user logoff}; events=%v", rec.kinds())
	}
}

func TestSessionTrustedIsExemptPreAuth(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.PreAuthIdle = 2 * time.Minute
	s.Idle = 30 * time.Minute
	s.PreAuthMax = 5 * time.Minute
	s.Trusted = true

	pipe, _ := net.Pipe()
	defer pipe.Close()
	client := &idleRecordingConn{Conn: pipe}
	s.Run(client)

	want := []string{"window:0s", "window:30m0s", "window:0s"}
	if !slices.Equal(client.calls, want) {
		t.Errorf("trusted regime calls = %v, want %v", client.calls, want)
	}
}

func TestSessionBridgeIdleExemptDisablesTimeout(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{
			{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}, choice: menuService},
			{quit: true},
		},
	}
	b := &fakeBridger{causes: []bridge.Cause{bridge.CauseUserEscaped}}
	s := newTestSession(t, p, b)
	s.PreAuthIdle = 2 * time.Minute
	s.Idle = 30 * time.Minute
	s.PreAuthMax = 5 * time.Minute
	s.BridgeIdleExempt = true

	pipe, _ := net.Pipe()
	defer pipe.Close()
	client := &idleRecordingConn{Conn: pipe}
	s.Run(client)

	if !slices.Contains(client.calls, "window:0s") {
		t.Errorf("bridge-exempt did not disable idle; calls=%v", client.calls)
	}
}

func TestSessionAuditsIdleTimeoutAtNegotiate(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		negErr:   os.ErrDeadlineExceeded,
	}
	s := newTestSession(t, p, &fakeBridger{})
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	disc := rec.events[len(rec.events)-1]
	if disc.Kind != store.AuditDisconnect || disc.Detail != "idle timeout" {
		t.Errorf("disconnect = %+v, want Detail %q", disc, "idle timeout")
	}
}

// Non-timeout presenter errors must keep their existing audit details.
func TestSessionNonTimeoutErrorsKeepDetails(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}},
		menuPicks: []menuResult{{err: net.ErrClosed}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	disc := rec.events[len(rec.events)-1]
	if disc.Detail != "menu render error" {
		t.Errorf("disconnect detail = %q, want %q", disc.Detail, "menu render error")
	}
}

// Idle timeout during a bridge surfaces as CauseClientClosed (the relay sees a
// read error); the session keeps its existing detail for that path.
func TestSessionBridgeTimeoutEndsAsClientClosed(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}},
		menuPicks: []menuResult{{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}, choice: menuService}},
	}
	b := &fakeBridger{causes: []bridge.Cause{bridge.CauseClientClosed}}
	s := newTestSession(t, p, b)
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	disc := rec.events[len(rec.events)-1]
	if disc.Detail != "client closed during bridge" {
		t.Errorf("disconnect detail = %q, want %q", disc.Detail, "client closed during bridge")
	}
}
