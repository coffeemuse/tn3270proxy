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
	"strings"
	"testing"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/CoffeeMuse/tn3270proxy/internal/ui3270"
	"github.com/racingmars/go3270"
)

func TestAuditEventColor(t *testing.T) {
	red := []string{store.AuditAuthFail, store.AuditAuthError, store.AuditMFAFailed}
	yellow := []string{store.AuditAdmin, store.AuditMFACleared, store.AuditMFAEnforced, store.AuditMFAEnrolled}
	plain := []string{store.AuditConnect, store.AuditAuthOK, store.AuditMFASuccess, store.AuditBridgeStart, store.AuditBridgeEnd, store.AuditLogout, store.AuditDisconnect}

	for _, k := range red {
		if c := auditEventColor(k); c != go3270.Red {
			t.Errorf("color(%s) = %v, want Red", k, c)
		}
	}
	for _, k := range yellow {
		if c := auditEventColor(k); c != go3270.Yellow {
			t.Errorf("color(%s) = %v, want Yellow", k, c)
		}
	}
	for _, k := range plain {
		if c := auditEventColor(k); c != go3270.DefaultColor {
			t.Errorf("color(%s) = %v, want Default", k, c)
		}
	}
}

func TestJulianStamp(t *testing.T) {
	// 2026-06-06 is day-of-year 157 (2026 is not a leap year).
	ts := time.Date(2026, 6, 6, 14, 32, 0, 0, time.UTC)
	if got := julianStamp(ts); got != "2026-06-06 (2026.157)" {
		t.Errorf("julianStamp = %q, want 2026-06-06 (2026.157)", got)
	}
}

func TestAuditLogFlow(t *testing.T) {
	p := &fakeAdminPresenter{}
	f, _ := newAdminFixture(t, p)
	f.resolver = fakeResolver{names: []string{"host.example.de."}}
	f.now = func() time.Time { return time.Date(2026, 6, 6, 14, 32, 0, 0, time.UTC) }

	ctx := context.Background()
	base := f.now().Add(-time.Hour)
	for _, ev := range []store.AuditEvent{
		{At: base, Kind: store.AuditAuthOK, Username: "ALICE", RemoteAddr: "203.0.113.9:5"},
		{At: base.Add(time.Minute), Kind: store.AuditAuthFail, Username: "BADGUY", RemoteAddr: "203.0.113.9:6", Detail: "delay=4s count=2"},
	} {
		if err := f.store.(*store.Store).RecordAudit(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	// Drive: S on row 0 (newest = AUTH_FAIL) → detail, then PF3 to exit.
	p.snaps = []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}}
	if err := f.auditLog(ctx, nil); err != nil {
		t.Fatal(err)
	}

	lv := p.gotSnaps[0]
	if !strings.Contains(lv.AsOf, "2026-06-06 (2026.157)") {
		t.Errorf("as-of stamp = %q", lv.AsOf)
	}
	// Mid is padded to the 12-col EVENT budget, so compare trimmed.
	if strings.TrimSpace(lv.Rows[0].Mid) != "AUTH_FAIL" || lv.Rows[0].MidColor != go3270.Red {
		t.Errorf("top row = %+v, want AUTH_FAIL red", lv.Rows[0])
	}
	dv := p.gotDets[0]
	var sawPTR bool
	for _, fld := range dv.Fields {
		if fld.Label == "PTR" && fld.Value == "host.example.de" {
			sawPTR = true
		}
	}
	if !sawPTR {
		t.Errorf("detail PTR missing; fields=%+v", dv.Fields)
	}
}

func TestAuditDetailShowsActor(t *testing.T) {
	f := &adminFlow{} // auditDetail only needs ctx + the event for the actor field
	dv := f.auditDetail(context.Background(), store.AuditEvent{
		Kind: store.AuditMFACleared, SessionID: "s1",
		Username: "BOB", Actor: "ADMIN"})

	var found bool
	for _, fld := range dv.Fields {
		if fld.Label == "Actor" && fld.Value == "ADMIN" {
			found = true
		}
	}
	if !found {
		t.Errorf("auditDetail fields missing Actor=ADMIN; got %+v", dv.Fields)
	}
}

func TestAuditEventColor_SessionDisconnectIsYellow(t *testing.T) {
	if got := auditEventColor(store.AuditSessionDisconnect); got != go3270.Yellow {
		t.Errorf("auditEventColor(session_disconnect) = %v, want Yellow", got)
	}
}
