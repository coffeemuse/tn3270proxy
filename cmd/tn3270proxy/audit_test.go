package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
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
		{"1d12h", 0, true},            // mixed day form not supported
		{"ninety", 0, true},
		{"9999999d", 0, true},          // overflow guard: > maxDays
		{" 90d", 0, true},              // leading whitespace rejected
		{" 24h", 0, true},              // leading whitespace rejected (time.ParseDuration rejects it too)
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
