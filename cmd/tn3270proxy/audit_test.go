package main

import (
	"testing"
	"time"
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
