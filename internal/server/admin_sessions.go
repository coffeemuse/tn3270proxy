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
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/CoffeeMuse/tn3270proxy/internal/ui3270"
)

// Active-sessions column widths (full-width row, cols 8–79 = 72 chars):
// ID 7 + CLIENT 21 + CONNECTED 9 + SESSION 9 + USER 8 + SERVICE 13, single
// spaces between (7+1+21+1+9+1+9+1+8+1+13 = 72). SESSION is 9 wide so the
// HH:MM:SS elapsed clock has room for 3-digit hours (a session held open for
// 100+ hours via bridge_idle=exempt) without shifting USER/SERVICE. ID is a
// process-lifetime monotonic serial (NOT bounded by max_conns, which caps only
// concurrent sessions) — 7 digits cover ~10M connections, and every column
// (ID included) is clip-guarded so an extreme value can never shift the row.
const sessionRowFmt = "%-7s %-21s %-9s %-9s %-8s %-13s"

func sessionHeader() string {
	return fmt.Sprintf("%-7s %-21s %-9s %-9s %-8s %-13s",
		"ID", "CLIENT", "CONNECTED", "SESSION", "USER", "SERVICE")
}

// clipField clips s to width w (in runes), marking truncation with a trailing
// '>' (ASCII; '…' is unsafe on a 3270 screen). Shorter strings are returned
// unchanged (the row formatter pads via the width verb). Rune-based so a
// non-ASCII value (e.g. an accented username) is never sliced mid-rune.
func clipField(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w-1]) + ">"
}

// hhmmss formats a non-negative duration as HH:MM:SS; hours are not zero-padded
// beyond two digits and may exceed 99 (the SESSION column reserves 9 chars).
func hhmmss(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", secs/3600, (secs%3600)/60, secs%60)
}

// fmtSessionRow packs one SessionView into a full-width snapshot row.
func fmtSessionRow(v SessionView, now time.Time, selfID uint64) ui3270.SnapshotRow {
	user := v.Username
	if v.LoggedInAt.IsZero() {
		user = "(login)"
	}
	if v.ID == selfID {
		user = "*YOU*"
	}
	service := v.Service
	if service == "" {
		service = "-"
	}
	left := fmt.Sprintf(sessionRowFmt,
		clipField(strconv.FormatUint(v.ID, 10), 7),
		clipField(v.RemoteAddr, 21),
		v.ConnectedAt.UTC().Format("15:04:05"),
		hhmmss(now.Sub(v.ConnectedAt)),
		clipField(user, 8),
		clipField(service, 13),
	)
	return ui3270.SnapshotRow{Left: left}
}

// activeSessions drives the read-only live-session viewer. The list is pure
// read-only nav: S opens a SESSION DETAIL screen where disconnect lives as a
// confirm-gated PF11 action. PF3 returns to the admin menu.
func (f *adminFlow) activeSessions(ctx context.Context, conn net.Conn) error {
	if f.sessions == nil {
		return nil // no registry wired (direct unit tests without one)
	}
	r := f.renderer(conn)
	cfg := ui3270.SnapshotConfig[SessionView]{
		Title:  "ACTIVE SESSIONS",
		Wide:   true,
		Head:   ui3270.SnapshotRow{Left: sessionHeader()},
		Legend: "S = detail",
		PFHelp: "PF3=Admin Menu  PF7=Up  PF8=Down  Enter=Refresh",
		Empty:  "(no active sessions)",
		Rows:   f.term.Rows,
		Fetch: func(ctx context.Context) ([]ui3270.SnapshotEntry[SessionView], string, string) {
			now := f.clock()
			views := f.sessions.Snapshot()
			rows := make([]ui3270.SnapshotEntry[SessionView], len(views))
			for i, v := range views {
				rows[i] = ui3270.SnapshotEntry[SessionView]{Row: fmtSessionRow(v, now, f.selfSessionID), Item: v}
			}
			asOf := "AS OF " + julianStamp(now) + "  " + now.Format("15:04") + " UTC"
			return rows, asOf, ""
		},
		OnSelect: func(ctx context.Context, r ui3270.Renderer, v SessionView) (bool, error) {
			return ui3270.RunDetail(ctx, r, ui3270.DetailConfig{
				View:       f.sessionDetail(ctx, v),
				ActPF:      11,
				DonePFHelp: "PF3=Back",
				Confirm: func() (string, string) {
					if v.ID == f.selfSessionID {
						return "", "CANNOT DISCONNECT YOUR OWN SESSION"
					}
					who := v.Username
					if who == "" {
						who = v.RemoteAddr
					}
					return "CONFIRM DISCONNECT " + who + " - PRESS PF11 AGAIN", ""
				},
				OnAct: func(ctx context.Context) (string, bool) {
					booted, ok := f.sessions.Disconnect(v.ID)
					if !ok {
						return "SESSION ALREADY ENDED", true
					}
					if f.audit != nil {
						f.audit(ctx, store.AuditEvent{
							Kind:     store.AuditSessionDisconnect,
							Username: booted.Username,
							Detail:   fmt.Sprintf("disconnected session %d (%s)", booted.ID, booted.RemoteAddr),
						})
					}
					return "DISCONNECTED", true
				},
			})
		},
	}
	return ui3270.RunSnapshotList(ctx, r, cfg)
}

// sessionDetail builds the read-only detail view for one live session: the full
// (untruncated) client address, an optional reverse-DNS PTR (when AUDIT_REVERSE_DNS
// is on — the same flag the audit detail uses), both timestamps with a Julian
// stamp and elapsed age, the user, and the bridged service.
func (f *adminFlow) sessionDetail(ctx context.Context, v SessionView) ui3270.DetailView {
	now := f.clock()
	connAt := v.ConnectedAt.UTC()

	loggedIn := "(not logged in)"
	user := "(login)"
	if !v.LoggedInAt.IsZero() {
		at := v.LoggedInAt.UTC()
		loggedIn = julianStamp(at) + " " + at.Format("15:04:05") + " UTC   (" + hhmmss(now.Sub(v.LoggedInAt)) + ")"
		user = v.Username
	}
	service := v.Service
	if service == "" {
		service = "-"
	}

	fields := []ui3270.DetailField{
		{Label: "Session", Value: strconv.FormatUint(v.ID, 10)},
		{Label: "Client", Value: v.RemoteAddr},
	}
	if f.auditReverseDNS(ctx) {
		res := f.resolver
		if res == nil {
			res = net.DefaultResolver
		}
		if name := ptr(ctx, res, v.RemoteAddr); name != "" {
			fields = append(fields, ui3270.DetailField{Label: "PTR", Value: name})
		}
	}
	fields = append(fields,
		ui3270.DetailField{Label: "Connected", Value: julianStamp(connAt) + " " + connAt.Format("15:04:05") + " UTC   (" + hhmmss(now.Sub(v.ConnectedAt)) + ")"},
		ui3270.DetailField{Label: "Logged in", Value: loggedIn},
		ui3270.DetailField{Label: "User", Value: user},
		ui3270.DetailField{Label: "Service", Value: service},
	)
	return ui3270.DetailView{
		Title:     "SESSION DETAIL",
		Fields:    fields,
		PFHelp:    "PF11=Disconnect   PF3=Back",
		DotLeader: true,
	}
}
