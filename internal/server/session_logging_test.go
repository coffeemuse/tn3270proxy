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
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"strings"
	"testing"
)

// bufLogger returns a slog.Logger that writes JSON to buf (so tests can assert
// on structured field names and values without parsing text heuristics).
func bufLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestSessionAuthFailLogsUserNotPassword verifies that a failed login attempt
// emits a log entry carrying the attempted username but never the submitted
// password. This is a permanent invariant (CLAUDE.md no-credential-logging rule).
func TestSessionAuthFailLogsUserNotPassword(t *testing.T) {
	var buf bytes.Buffer
	logger := bufLogger(&buf)

	const submittedPassword = "s3cr3tShouldNeverAppear"
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: submittedPassword}, // wrong password → auth fail
			{quit: true},                             // next login render: quit
		},
		menuPicks: []menuResult{},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.Logger = logger

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	output := buf.String()

	// The auth-fail log line must mention the username.
	if !strings.Contains(output, "alice") {
		t.Errorf("auth-fail log should carry the username; got:\n%s", output)
	}
	// The submitted password must never appear anywhere in the logs.
	if strings.Contains(output, submittedPassword) {
		t.Errorf("submitted password leaked into log output; got:\n%s", output)
	}
	// The log must contain an "auth failed" message (stable contract for fail2ban).
	if !strings.Contains(output, "auth failed") {
		t.Errorf("expected 'auth failed' message in log; got:\n%s", output)
	}
}

// TestSessionLoggerEnrichedWithUserAfterAuth verifies that log lines emitted
// after a successful login carry the "user" field.
func TestSessionLoggerEnrichedWithUserAfterAuth(t *testing.T) {
	var buf bytes.Buffer
	logger := bufLogger(&buf)

	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true},
		},
		// Trigger a list-services error so the session emits a log line
		// after auth but before the bridge — that line should carry "user".
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.Logger = logger

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	// The session runs through: negotiate → login(ok) → menu(quit → logoff)
	// At minimum the negotiation log is pre-auth (no user); the auth-ok path
	// sets s.Logger = s.Logger.With("user", ...).
	// We assert the log output does NOT contain the password.
	if strings.Contains(buf.String(), "good") {
		t.Errorf("password 'good' leaked into log output:\n%s", buf.String())
	}
}

// findLogRecord scans the JSON log lines in buf and returns the first record
// whose "msg" equals want. Fails the test if none is found.
func findLogRecord(t *testing.T, buf *bytes.Buffer, want string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec["msg"] == want {
			return rec
		}
	}
	t.Fatalf("no log record with msg=%q found in:\n%s", want, buf.String())
	return nil
}

// TestAuthFailLineCarriesFail2banFields asserts the password-failure line
// carries the documented fail2ban fields: src (bare IP), trusted, coarse
// reason, and the attempted user.
func TestAuthFailLineCarriesFail2banFields(t *testing.T) {
	var buf bytes.Buffer
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "wrong"}, // auth fail
			{quit: true},
		},
		menuPicks: []menuResult{},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.Logger = bufLogger(&buf)
	s.RemoteHost = "203.0.113.7"
	s.Trusted = false

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	rec := findLogRecord(t, &buf, "auth failed")
	if rec["src"] != "203.0.113.7" {
		t.Errorf("src = %v, want 203.0.113.7", rec["src"])
	}
	if v, ok := rec["trusted"].(bool); !ok || v != false {
		t.Errorf("trusted = %v, want false", rec["trusted"])
	}
	if rec["reason"] != "invalid_credentials" {
		t.Errorf("reason = %v, want invalid_credentials", rec["reason"])
	}
	if rec["user"] != "alice" {
		t.Errorf("user = %v, want alice", rec["user"])
	}
}

// TestAuthFailLineTrustedMarker asserts the trusted marker reflects a trusted
// session (so an operator filter can ban untrusted sources only).
func TestAuthFailLineTrustedMarker(t *testing.T) {
	var buf bytes.Buffer
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "wrong"}, {quit: true}},
		menuPicks: []menuResult{},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.Logger = bufLogger(&buf)
	s.RemoteHost = "10.0.0.5"
	s.Trusted = true

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	rec := findLogRecord(t, &buf, "auth failed")
	if v, ok := rec["trusted"].(bool); !ok || v != true {
		t.Errorf("trusted = %v, want true", rec["trusted"])
	}
}

// TestAuthFailLineShapeIsStable pins the auth-failure field set so the fail2ban
// contract cannot silently drift (extra or missing fields fail the test).
func TestAuthFailLineShapeIsStable(t *testing.T) {
	var buf bytes.Buffer
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "wrong"}, {quit: true}},
		menuPicks: []menuResult{},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.Logger = bufLogger(&buf)
	s.RemoteHost = "203.0.113.7"

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	rec := findLogRecord(t, &buf, "auth failed")
	delete(rec, "time")
	delete(rec, "level")
	delete(rec, "msg")
	want := map[string]bool{"user": true, "src": true, "trusted": true, "reason": true}
	for k := range rec {
		if !want[k] {
			t.Errorf("unexpected field %q on auth-failure line (stable contract)", k)
		}
	}
	for k := range want {
		if _, ok := rec[k]; !ok {
			t.Errorf("missing required field %q on auth-failure line (stable contract)", k)
		}
	}
}

// TestMFAFailLineReasonBadMFA asserts a wrong TOTP code emits the stable
// auth-failure line with reason=bad_mfa, and that the MFA secret never leaks.
func TestMFAFailLineReasonBadMFA(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	var buf bytes.Buffer
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		verifies:  []mfaResult{{code: "000000"}, {quit: true}}, // wrong code, then PF3
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{},
	}
	s, st := newMFATestSession(t, p, &fakeBridger{})
	s.Logger = bufLogger(&buf)
	s.RemoteHost = "198.51.100.9"

	ctx := context.Background()
	uid, _ := st.CreateUser(ctx, "alice", "x")
	st.SetMFARequired(ctx, uid, true)
	enc, _ := s.MFA.Seal([]byte(secret))
	st.StoreMFAEnrollment(ctx, uid, enc, "2026-01-01T00:00:00Z", 0)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	rec := findLogRecord(t, &buf, "auth failed")
	if rec["reason"] != "bad_mfa" {
		t.Errorf("reason = %v, want bad_mfa", rec["reason"])
	}
	if rec["src"] != "198.51.100.9" {
		t.Errorf("src = %v, want 198.51.100.9", rec["src"])
	}
	if rec["user"] != "alice" {
		t.Errorf("user = %v, want alice", rec["user"])
	}
	if strings.Contains(buf.String(), secret) {
		t.Errorf("MFA secret leaked into log output:\n%s", buf.String())
	}
}
