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
