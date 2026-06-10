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
	"context"
	"errors"
	"net"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/coffeemuse/tn3270proxy/internal/store"
)

// motdSession builds a test session whose MOTD_FILE is set to path and whose
// MOTDRead returns (data, readErr). readCalled (if non-nil) is set true when
// the reader is invoked, so tests can assert the reader was skipped.
func motdSession(t *testing.T, p *fakePresenter, path string, data []byte, readErr error, readCalled *bool) *Session {
	t.Helper()
	s := newTestSession(t, p, &fakeBridger{})
	// MOTD_FILE is seeded empty by migrate(); set it for the test.
	if err := s.Store.SetConfig(context.Background(), "MOTD_FILE", path); err != nil {
		t.Fatalf("SetConfig MOTD_FILE: %v", err)
	}
	s.MOTDRead = func(string) ([]byte, error) {
		if readCalled != nil {
			*readCalled = true
		}
		return data, readErr
	}
	return s
}

func TestMOTDShownBeforeMenu(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // second login render after menu logoff
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := motdSession(t, p, "/etc/motd.txt", []byte("hello\nworld"), nil, nil)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.newsCalls) != 1 {
		t.Fatalf("News called %d times, want 1", len(p.newsCalls))
	}
	pages := p.newsCalls[0]
	if len(pages) != 1 || len(pages[0]) != 2 ||
		pages[0][0] != "hello" || pages[0][1] != "world" {
		t.Errorf("News pages = %#v, want one page [hello world]", pages)
	}
}

func TestMOTDUnsetSkips(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // second login render after menu logoff
		},
		menuPicks: []menuResult{{quit: true}},
	}
	// MOTD_FILE stays at its seeded default "".
	s := newTestSession(t, p, &fakeBridger{})

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.newsCalls) != 0 {
		t.Errorf("News called %d times, want 0 when MOTD_FILE unset", len(p.newsCalls))
	}
}

func TestMOTDRelativePathSkipsWithoutReading(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // second login render after menu logoff
		},
		menuPicks: []menuResult{{quit: true}},
	}
	read := false
	s := motdSession(t, p, "relative/motd.txt", []byte("x"), nil, &read)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if read {
		t.Error("reader was called for a relative path; want skipped before read")
	}
	if len(p.newsCalls) != 0 {
		t.Errorf("News called %d times, want 0 for relative path", len(p.newsCalls))
	}
}

func TestMOTDReadErrorSkips(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // second login render after menu logoff
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := motdSession(t, p, "/etc/motd.txt", nil, errors.New("boom"), nil)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.newsCalls) != 0 {
		t.Errorf("News called %d times, want 0 on read error", len(p.newsCalls))
	}
}

func TestMOTDEmptyFileSkips(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // second login render after menu logoff
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := motdSession(t, p, "/etc/motd.txt", []byte("   \n\n"), nil, nil)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.newsCalls) != 0 {
		t.Errorf("News called %d times, want 0 for whitespace-only file", len(p.newsCalls))
	}
}

func TestMOTDIdleTimeoutLogsOut(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // re-presented login after idle-logout
		},
	}
	s := motdSession(t, p, "/etc/motd.txt", []byte("news"), nil, nil)
	p.newsResults = []error{os.ErrDeadlineExceeded}
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

func TestMOTDIdleTimeoutRearmsPreAuth(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // re-presented login after the idle-logout
		},
	}
	s := motdSession(t, p, "/etc/motd.txt", []byte("news"), nil, nil)
	s.PreAuthIdle = 2 * time.Minute
	s.Idle = 30 * time.Minute
	s.PreAuthMax = 5 * time.Minute
	p.newsResults = []error{os.ErrDeadlineExceeded}

	pipe, _ := net.Pipe()
	defer pipe.Close()
	client := &idleRecordingConn{Conn: pipe}
	s.Run(client)

	want := []string{
		"preauth:2m0s/5m0s", // connect → pre-auth
		"window:30m0s",      // auth ok → post-auth
		"preauth:2m0s/5m0s", // MOTD idle-logout → back to pre-auth
	}
	if !slices.Equal(client.calls, want) {
		t.Errorf("regime calls = %v, want %v", client.calls, want)
	}
}

func TestMOTDRenderErrorDisconnects(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{user: "alice", pass: "good"}},
	}
	s := motdSession(t, p, "/etc/motd.txt", []byte("news"), nil, nil)
	p.newsResults = []error{errors.New("stream broke")}
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	var disc *store.AuditEvent
	for i := range rec.events {
		if rec.events[i].Kind == store.AuditDisconnect {
			disc = &rec.events[i]
		}
	}
	if disc == nil || disc.Detail != "news render error" {
		t.Errorf("disconnect = %+v, want Detail %q", disc, "news render error")
	}
}
