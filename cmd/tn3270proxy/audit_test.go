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

package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coffeemuse/tn3270proxy/internal/store"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"90d", 90 * 24 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"24h", 24 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"0d", 0, false},
		{"", 0, true},
		{"d", 0, true},
		{"-5d", 0, true},
		{"1d12h", 0, true}, // mixed day form not supported
		{"ninety", 0, true},
		{"9999999d", 0, true},              // overflow guard: > maxDays
		{" 90d", 0, true},                  // leading whitespace rejected
		{" 24h", 0, true},                  // leading whitespace rejected (time.ParseDuration rejects it too)
		{"+5d", 5 * 24 * time.Hour, false}, // leading + accepted by strconv.Atoi — deliberately valid
	}
	for _, c := range cases {
		got, err := parseDuration(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseDuration(%q) err = %v, wantErr %v", c.in, err, c.wantErr)
			continue
		}
		if !c.wantErr && got != c.want {
			t.Errorf("parseDuration(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestRunAuditDispatch(t *testing.T) {
	if err := runAudit(nil); err == nil {
		t.Error("no verb: want usage error")
	}
	if err := runAudit([]string{"bogus"}); err == nil {
		t.Error("unknown verb: want error")
	}
}

func TestRunAuditListBadSince(t *testing.T) {
	if err := runAuditList([]string{"-since", "ninety"}); err == nil {
		t.Error("invalid -since: want error")
	}
}

func TestPrintAuditEvents(t *testing.T) {
	at := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	events := []store.AuditEvent{
		{At: at, SessionID: "deadbeef00000000", Kind: store.AuditAuthOK,
			Username: "alice", RemoteAddr: "10.0.0.5:40000"},
		{At: at, SessionID: "deadbeef00000000", Kind: store.AuditBridgeEnd,
			Username: "alice", RemoteAddr: "10.0.0.5:40000", Service: "PROD", Detail: "user_escaped"},
	}
	var buf bytes.Buffer
	printAuditEvents(&buf, events)
	out := buf.String()
	for _, want := range []string{"2026-06-03T10:00:00Z", "auth_ok", "deadbeef00000000",
		"alice", "10.0.0.5:40000", "PROD", "user_escaped"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "\n"); n != 2 {
		t.Errorf("output lines = %d, want 2:\n%s", n, out)
	}
}

func TestPrintAuditEventsEmpty(t *testing.T) {
	var buf bytes.Buffer
	printAuditEvents(&buf, nil)
	if got := buf.String(); got != "no audit events\n" {
		t.Errorf("printAuditEvents(nil) = %q, want %q", got, "no audit events\n")
	}
}

func TestRunAuditPruneRequiresOlderThan(t *testing.T) {
	db := filepath.Join(t.TempDir(), "p.db")
	if err := runAuditPrune([]string{"-db", db}); err == nil {
		t.Error("missing -older-than: want error (no default that silently deletes)")
	}
}

func TestRunAuditPruneRejectsZeroDuration(t *testing.T) {
	db := filepath.Join(t.TempDir(), "p.db")
	for _, zero := range []string{"0d", "0h"} {
		if err := runAuditPrune([]string{"-db", db, "-older-than", zero}); err == nil {
			t.Errorf("-older-than %q: want error, got nil (would delete all rows)", zero)
		}
	}
}

func TestPrintAuditEventsIncludesActor(t *testing.T) {
	var buf bytes.Buffer
	printAuditEvents(&buf, []store.AuditEvent{
		{At: time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC),
			Kind: store.AuditMFACleared, SessionID: "s1",
			Username: "BOB", Actor: "ADMIN", RemoteAddr: "10.0.0.5:40000"},
	})
	out := buf.String()
	if !strings.Contains(out, "ADMIN") {
		t.Errorf("printed output missing actor; got:\n%s", out)
	}
	if !strings.Contains(out, "BOB") {
		t.Errorf("printed output missing subject; got:\n%s", out)
	}
}

func TestRunAuditPruneDeletesOldRows(t *testing.T) {
	db := filepath.Join(t.TempDir(), "p.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	old := store.AuditEvent{At: time.Now().Add(-100 * 24 * time.Hour), SessionID: "old", Kind: store.AuditConnect}
	fresh := store.AuditEvent{At: time.Now(), SessionID: "new", Kind: store.AuditConnect}
	if err := st.RecordAudit(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordAudit(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	st.Close()

	if err := runAuditPrune([]string{"-db", db, "-older-than", "90d"}); err != nil {
		t.Fatalf("runAuditPrune: %v", err)
	}

	st2, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	got, err := st2.ListAudit(ctx, store.AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SessionID != "new" {
		t.Errorf("remaining = %+v, want only session 'new'", got)
	}
}
