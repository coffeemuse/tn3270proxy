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

// Package logging provides a configurable slog-based logger for the proxy.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// ParseLevel parses s as a log level (case-insensitive).
// Valid values are "error", "warn", "info", and "debug".
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "error":
		return slog.LevelError, nil
	case "warn":
		return slog.LevelWarn, nil
	case "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	default:
		return 0, fmt.Errorf("unknown log level %q (want \"error\", \"warn\", \"info\", or \"debug\")", s)
	}
}

// multiHandler fans one slog record out to multiple handlers.
// Enabled returns true if any child handler is enabled for the level.
// Handle dispatches to each enabled child; all enabled children are attempted
// even if one returns an error (last error is returned).
// WithAttrs and WithGroup propagate to every child so logger.With(...) reaches
// all streams.
type multiHandler struct {
	handlers []slog.Handler
}

func (m multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var last error
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r); err != nil {
				last = err
			}
		}
	}
	return last
}

func (m multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithAttrs(attrs)
	}
	return multiHandler{handlers: hs}
}

func (m multiHandler) WithGroup(name string) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithGroup(name)
	}
	return multiHandler{handlers: hs}
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

// New creates a *slog.Logger that writes human-readable text to stderr (always)
// and, when file is non-empty, also writes machine-readable JSON to that file.
// Both streams honour the same level. Returns the logger, a Closer for the log
// file (no-op when no file is configured), and any error opening the file.
func New(level slog.Level, file string) (*slog.Logger, io.Closer, error) {
	opts := &slog.HandlerOptions{Level: level}
	console := slog.NewTextHandler(os.Stderr, opts)
	if file == "" {
		return slog.New(console), nopCloser{}, nil
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file %q: %w", file, err)
	}
	fileH := slog.NewJSONHandler(f, opts)
	handler := multiHandler{handlers: []slog.Handler{console, fileH}}
	return slog.New(handler), f, nil
}
