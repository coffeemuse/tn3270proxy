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
	"testing"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

func TestAuditTrailAutoFillsActor(t *testing.T) {
	rec := &recordingAuditor{}
	tr := &auditTrail{auditor: rec, sessionID: "sid", remoteAddr: "1.2.3.4:5"}
	ctx := context.Background()

	tr.record(ctx, store.AuditEvent{Kind: store.AuditConnect}) // pre-auth: no actor
	tr.setActor("ALICE")                                       // login success
	tr.record(ctx, store.AuditEvent{Kind: store.AuditAuthOK, Username: "ALICE"})
	tr.record(ctx, store.AuditEvent{Kind: store.AuditAdmin, Actor: "ADMIN"}) // explicit override kept
	tr.setActor("")                                                          // logout / back to login
	tr.record(ctx, store.AuditEvent{Kind: store.AuditDisconnect})

	if got := rec.events[0].Actor; got != "" {
		t.Errorf("connect actor = %q, want empty", got)
	}
	if got := rec.events[1].Actor; got != "ALICE" {
		t.Errorf("auth_ok actor = %q, want ALICE", got)
	}
	if got := rec.events[2].Actor; got != "ADMIN" {
		t.Errorf("admin actor = %q, want ADMIN (explicit, not clobbered)", got)
	}
	if got := rec.events[3].Actor; got != "" {
		t.Errorf("disconnect actor = %q, want empty after clear", got)
	}
}

func TestStoreAuditorRecordsAndStampsTime(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := storeAuditor{store: st}

	a.Record(context.Background(), store.AuditEvent{
		Kind: store.AuditConnect, SessionID: "abcd", RemoteAddr: "10.0.0.5:40000"})

	evs, err := st.ListAudit(context.Background(), store.AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Kind != store.AuditConnect || evs[0].At.IsZero() {
		t.Errorf("events = %+v, want one connect with a stamped time", evs)
	}
}
