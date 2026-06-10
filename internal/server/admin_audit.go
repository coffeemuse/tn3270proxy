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
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/coffeemuse/tn3270proxy/internal/store"
	"github.com/coffeemuse/tn3270proxy/internal/sysconfig"
	"github.com/coffeemuse/tn3270proxy/internal/ui3270"
	"github.com/racingmars/go3270"
)

// auditEventColor maps an audit kind to its display colour (derived severity;
// there is no stored severity). Only failures (red) and security-state changes
// (yellow) are coloured; routine events stay plain so the exceptions pop.
func auditEventColor(kind string) go3270.Color {
	switch kind {
	case store.AuditAuthFail, store.AuditAuthError, store.AuditMFAFailed:
		return go3270.Red
	case store.AuditAdmin, store.AuditMFACleared, store.AuditMFAEnforced, store.AuditMFAEnrolled, store.AuditSessionDisconnect:
		return go3270.Yellow
	default:
		return go3270.DefaultColor
	}
}

// julianStamp formats t as "YYYY-MM-DD (YYYY.DDD)" with the day-of-year, used on
// both the list as-of header and the detail Date/Time line.
func julianStamp(t time.Time) string {
	return fmt.Sprintf("%s (%d.%03d)", t.Format("2006-01-02"), t.Year(), t.YearDay())
}

// auditCap reads AUDIT_MAX_ROWS, clamped to [1,10000] with a fallback to the
// default for a hand-edited DB (mirrors Session.throttleInt).
func (f *adminFlow) auditCap(ctx context.Context) int {
	v, err := f.store.GetConfig(ctx, sysconfig.KeyAuditMaxRows)
	if err != nil {
		return sysconfig.DefaultAuditMaxRows
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 || n > 10000 {
		return sysconfig.DefaultAuditMaxRows
	}
	return n
}

// auditReverseDNS reports whether the reverse-DNS lookup is enabled
// (AUDIT_REVERSE_DNS == "Y"; default on when unreadable).
func (f *adminFlow) auditReverseDNS(ctx context.Context) bool {
	v, err := f.store.GetConfig(ctx, sysconfig.KeyAuditReverseDNS)
	if err != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(v), "Y")
}

// auditWindow is the bounded look-back of the RECENT view.
const auditWindow = 72 * time.Hour

// auditLog drives the read-only RECENT activity viewer (GH #40): a 72h snapshot,
// newest-first, paged, with an 'S' drill-down to a PTR-enriched detail screen.
func (f *adminFlow) auditLog(ctx context.Context, conn net.Conn) error {
	r := f.renderer(conn)
	maxRows := f.auditCap(ctx)
	cfg := ui3270.SnapshotConfig[store.AuditEvent]{
		Title:  "RECENT ACTIVITY",
		Head:   ui3270.SnapshotRow{Left: "MM/DD HH:MM USERNAME", Mid: "EVENT", Right: "DETAIL"},
		Legend: "S = detail",
		PFHelp: "PF3=Admin Menu   PF7=PgUp  PF8=PgDn   Enter=Refresh",
		Empty:  "(no activity in the last 72 hours)",
		Rows:   f.term.Rows,
		Fetch: func(ctx context.Context) ([]ui3270.SnapshotEntry[store.AuditEvent], string, string) {
			now := f.clock()
			evs, err := f.store.ListAudit(ctx, store.AuditFilter{
				Since: now.Add(-auditWindow),
				Limit: maxRows + 1, // +1 detects window overflow
			})
			if err != nil {
				return nil, "", f.storeErr("list audit", err)
			}
			asOf := "AS OF " + julianStamp(now) + "  " + now.Format("15:04") + " UTC"
			if len(evs) > maxRows {
				evs = evs[:maxRows]
				asOf += fmt.Sprintf("  (NEWEST %d)", maxRows)
			}
			rows := make([]ui3270.SnapshotEntry[store.AuditEvent], len(evs))
			for i, ev := range evs {
				rows[i] = ui3270.SnapshotEntry[store.AuditEvent]{Row: auditRow(ev), Item: ev}
			}
			return rows, asOf, ""
		},
		OnSelect: func(ctx context.Context, r ui3270.Renderer, ev store.AuditEvent) (bool, error) {
			return false, r.Detail(f.auditDetail(ctx, ev))
		},
	}
	return ui3270.RunSnapshotList(ctx, r, cfg)
}

// clock returns the flow's current time (now seam; nil → time.Now), in UTC.
func (f *adminFlow) clock() time.Time {
	if f.now != nil {
		return f.now().UTC()
	}
	return time.Now().UTC()
}

// auditRow formats one event into the list's three coloured segments. The Mid
// (event) segment is the uppercased kind; Left/Right are truncated to budget.
func auditRow(ev store.AuditEvent) ui3270.SnapshotRow {
	at := ev.At.UTC()
	left := fmt.Sprintf("%s %s %-8.8s", at.Format("01/02"), at.Format("15:04"), ev.Username)
	return ui3270.SnapshotRow{
		Left:     left,
		Mid:      fmt.Sprintf("%-12.12s", strings.ToUpper(ev.Kind)),
		Right:    truncate(ev.Detail, 38),
		MidColor: auditEventColor(ev.Kind),
	}
}

// auditDetail builds the read-only detail view for one event, resolving the PTR
// when reverse DNS is enabled and the event carries a remote address.
func (f *adminFlow) auditDetail(ctx context.Context, ev store.AuditEvent) ui3270.DetailView {
	at := ev.At.UTC()
	fields := []ui3270.DetailField{
		{Label: "Date/Time", Value: julianStamp(at) + " " + at.Format("15:04:05") + " UTC"},
		{Label: "Session", Value: ev.SessionID},
		{Label: "Username", Value: ev.Username},
		{Label: "Actor", Value: ev.Actor},
		{Label: "Event", Value: strings.ToUpper(ev.Kind), Color: auditEventColor(ev.Kind)},
	}
	if ev.RemoteAddr != "" {
		fields = append(fields, ui3270.DetailField{Label: "Remote", Value: ev.RemoteAddr})
		if f.auditReverseDNS(ctx) {
			res := f.resolver
			if res == nil {
				res = net.DefaultResolver
			}
			if name := ptr(ctx, res, ev.RemoteAddr); name != "" {
				fields = append(fields, ui3270.DetailField{Label: "PTR", Value: name})
			}
		}
	}
	fields = append(fields, ui3270.DetailField{Label: "Service", Value: ev.Service})
	return ui3270.DetailView{
		Title:     "AUDIT DETAIL",
		Fields:    fields,
		BodyLabel: "Detail",
		Body:      ev.Detail,
		PFHelp:    "PF3=Back",
		DotLeader: true,
	}
}

// truncate limits s to n runes (the list summary; full text is on the detail).
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
