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
	"testing"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/bridge"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// idleRecordingConn wraps a net.Conn and records SetIdle calls, standing in
// for the idleConn wrapper installed by the accept path.
type idleRecordingConn struct {
	net.Conn
	setIdleCalls []time.Duration
}

func (c *idleRecordingConn) SetIdle(d time.Duration) {
	c.setIdleCalls = append(c.setIdleCalls, d)
}

func TestSessionSwitchesIdleAfterAuthAndBackOnLogoff(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // second login render after menu logoff
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.PreAuthIdle = 2 * time.Minute
	s.Idle = 30 * time.Minute

	pipe, _ := net.Pipe()
	defer pipe.Close()
	client := &idleRecordingConn{Conn: pipe}
	s.Run(client)

	want := []time.Duration{30 * time.Minute, 2 * time.Minute} // post-auth, then logoff
	if len(client.setIdleCalls) != 2 ||
		client.setIdleCalls[0] != want[0] || client.setIdleCalls[1] != want[1] {
		t.Errorf("SetIdle calls = %v, want %v", client.setIdleCalls, want)
	}
}

func TestSessionZeroIdleConfigLeavesConnAlone(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{}) // PreAuthIdle/Idle left zero

	pipe, _ := net.Pipe()
	defer pipe.Close()
	client := &idleRecordingConn{Conn: pipe}
	s.Run(client)

	if len(client.setIdleCalls) != 0 {
		t.Errorf("SetIdle calls = %v, want none when idle config is zero", client.setIdleCalls)
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

func TestSessionAuditsIdleTimeoutAtMenu(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}},
		menuPicks: []menuResult{{err: os.ErrDeadlineExceeded}},
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
	if disc.Username != "alice" {
		t.Errorf("disconnect username = %q, want alice (timed out post-auth)", disc.Username)
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
		menuPicks: []menuResult{{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}}},
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
