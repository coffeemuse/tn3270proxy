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

package logging

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLevelValid(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"info", slog.LevelInfo},
		{"Info", slog.LevelInfo},
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseLevel(tc.in)
			if err != nil {
				t.Fatalf("ParseLevel(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseLevelInvalid(t *testing.T) {
	for _, s := range []string{"trace", "fatal", "", "INFO ", "42"} {
		t.Run(s, func(t *testing.T) {
			if _, err := ParseLevel(s); err == nil {
				t.Errorf("ParseLevel(%q) want error, got nil", s)
			}
		})
	}
}

func TestMultiHandlerFanOut(t *testing.T) {
	var b1, b2 bytes.Buffer
	h1 := slog.NewTextHandler(&b1, &slog.HandlerOptions{Level: slog.LevelDebug})
	h2 := slog.NewTextHandler(&b2, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(multiHandler{handlers: []slog.Handler{h1, h2}})
	logger.Info("hello")
	if !strings.Contains(b1.String(), "hello") {
		t.Error("b1: expected 'hello'")
	}
	if !strings.Contains(b2.String(), "hello") {
		t.Error("b2: expected 'hello'")
	}
}

func TestMultiHandlerLevelFilter(t *testing.T) {
	var low, high bytes.Buffer
	hLow := slog.NewTextHandler(&low, &slog.HandlerOptions{Level: slog.LevelDebug})
	hHigh := slog.NewTextHandler(&high, &slog.HandlerOptions{Level: slog.LevelError})
	logger := slog.New(multiHandler{handlers: []slog.Handler{hLow, hHigh}})
	logger.Info("info-msg")
	if !strings.Contains(low.String(), "info-msg") {
		t.Error("low handler should receive Info")
	}
	if strings.Contains(high.String(), "info-msg") {
		t.Error("high handler should not receive Info (level is Error)")
	}
}

func TestMultiHandlerWithAttrsPropagates(t *testing.T) {
	var b1, b2 bytes.Buffer
	h1 := slog.NewTextHandler(&b1, &slog.HandlerOptions{Level: slog.LevelDebug})
	h2 := slog.NewTextHandler(&b2, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(multiHandler{handlers: []slog.Handler{h1, h2}}).With("remote", "1.2.3.4:5678")
	logger.Info("connected")
	for i, buf := range []*bytes.Buffer{&b1, &b2} {
		if !strings.Contains(buf.String(), "1.2.3.4:5678") {
			t.Errorf("handler %d: WithAttrs not propagated", i+1)
		}
	}
}

func TestMultiHandlerEnabled(t *testing.T) {
	var b bytes.Buffer
	h := slog.NewTextHandler(&b, &slog.HandlerOptions{Level: slog.LevelWarn})
	m := multiHandler{handlers: []slog.Handler{h}}
	ctx := context.Background()
	if m.Enabled(ctx, slog.LevelInfo) {
		t.Error("Info should not be enabled when handler level is Warn")
	}
	if !m.Enabled(ctx, slog.LevelError) {
		t.Error("Error should be enabled when handler level is Warn")
	}
}

func TestNewConsoleOnly(t *testing.T) {
	logger, closer, err := New(slog.LevelInfo, "")
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	if err := closer.Close(); err != nil {
		t.Errorf("nopCloser.Close() = %v, want nil", err)
	}
}

func TestNewWithFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Redirect stderr to /dev/null to suppress console output during the test.
	old := os.Stderr
	devNull, _ := os.Open(os.DevNull)
	os.Stderr = devNull
	logger, closer, err := New(slog.LevelDebug, path)
	os.Stderr = old
	devNull.Close()

	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	defer closer.Close()

	logger.Info("test-event", "key", "val")
	closer.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "test-event") {
		t.Errorf("log file does not contain test-event: %s", data)
	}
	if !strings.Contains(string(data), `"key"`) {
		t.Errorf("log file does not contain key field: %s", data)
	}
}

func TestNewWithFileBadPath(t *testing.T) {
	_, closer, err := New(slog.LevelInfo, "/no/such/dir/x.log")
	if err == nil {
		closer.Close()
		t.Fatal("expected error for bad path, got nil")
	}
}

func TestNewLevelFiltering(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "level.log")

	old := os.Stderr
	devNull, _ := os.Open(os.DevNull)
	os.Stderr = devNull
	logger, closer, err := New(slog.LevelInfo, path)
	os.Stderr = old
	devNull.Close()

	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	defer closer.Close()

	logger.Debug("debug-should-not-appear")
	logger.Info("info-should-appear")
	closer.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(data), "debug-should-not-appear") {
		t.Error("debug message appeared when level is Info")
	}
	if !strings.Contains(string(data), "info-should-appear") {
		t.Error("info message did not appear when level is Info")
	}
}
