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
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// parseDuration is time.ParseDuration plus a "d" suffix (days, 24h each),
// e.g. "90d" or "36h". Mixed forms like "1d12h" are not supported.
// time.ParseDuration stops at hours, and "2160h" is operator-hostile.
func parseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid duration %q (want e.g. 90d or 24h)", s)
		}
		const maxDays = 36500 // ~100 years; far beyond any sane retention window
		if n > maxDays {
			return 0, fmt.Errorf("invalid duration %q (max %dd)", s, maxDays)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration %q (want e.g. 90d or 24h)", s)
	}
	return d, nil
}

// runAudit dispatches the audit verbs.
func runAudit(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("audit: usage: audit list|prune [flags]")
	}
	switch args[0] {
	case "list":
		return runAuditList(args[1:])
	case "prune":
		return runAuditPrune(args[1:])
	default:
		return fmt.Errorf("audit: unknown subcommand %q (want list or prune)", args[0])
	}
}

func runAuditList(args []string) error {
	fs := flag.NewFlagSet("audit list", flag.ContinueOnError)
	dbPath := fs.String("db", "tn3270proxy.db", "path to SQLite database file")
	user := fs.String("user", "", "filter by subject username")
	actor := fs.String("actor", "", "filter by acting principal (who performed the action)")
	kind := fs.String("kind", "", "filter by event kind (e.g. auth_fail)")
	since := fs.String("since", "", "only events newer than this age (e.g. 24h, 7d)")
	limit := fs.Int("limit", 100, "maximum rows")
	if err := fs.Parse(args); err != nil {
		return err
	}
	f := store.AuditFilter{Username: *user, Actor: *actor, Kind: *kind, Limit: *limit}
	if *since != "" {
		d, err := parseDuration(*since)
		if err != nil {
			return fmt.Errorf("audit list: -since: %w", err)
		}
		f.Since = time.Now().Add(-d)
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	events, err := st.ListAudit(context.Background(), f)
	if err != nil {
		return err
	}
	printAuditEvents(os.Stdout, events)
	return nil
}

// printAuditEvents writes one event per line: time, kind, session, subject user,
// actor, remote, service, detail.
func printAuditEvents(w io.Writer, events []store.AuditEvent) {
	if len(events) == 0 {
		fmt.Fprintln(w, "no audit events")
		return
	}
	for _, ev := range events {
		fmt.Fprintf(w, "%s  %-12s %s  %-12s %-12s %-21s %-12s %s\n",
			ev.At.Format(time.RFC3339), ev.Kind, ev.SessionID,
			ev.Username, ev.Actor, ev.RemoteAddr, ev.Service, ev.Detail)
	}
}

func runAuditPrune(args []string) error {
	fs := flag.NewFlagSet("audit prune", flag.ContinueOnError)
	dbPath := fs.String("db", "tn3270proxy.db", "path to SQLite database file")
	olderThan := fs.String("older-than", "", "delete events older than this age (e.g. 90d; required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *olderThan == "" {
		return fmt.Errorf("audit prune: -older-than is required")
	}
	d, err := parseDuration(*olderThan)
	if err != nil {
		return fmt.Errorf("audit prune: -older-than: %w", err)
	}
	if d == 0 {
		return fmt.Errorf("audit prune: -older-than must be positive (got %q)", *olderThan)
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	n, err := st.PruneAudit(context.Background(), time.Now().Add(-d))
	if err != nil {
		return err
	}
	fmt.Printf("pruned %d audit rows\n", n)
	return nil
}
