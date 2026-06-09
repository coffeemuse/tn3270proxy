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
	"net"
	"strings"
	"testing"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/CoffeeMuse/tn3270proxy/internal/ui3270"
)

// fakeRegistry is a scripted SessionRegistry.
type fakeRegistry struct {
	views       []SessionView
	disconnect  []uint64 // ids passed to Disconnect, in order
	disconnects map[uint64]SessionView
	okFor       map[uint64]bool
}

func (f *fakeRegistry) Snapshot() []SessionView { return f.views }
func (f *fakeRegistry) Disconnect(id uint64) (SessionView, bool) {
	f.disconnect = append(f.disconnect, id)
	return f.disconnects[id], f.okFor[id]
}

// sessRenderer scripts Snapshot actions for the activeSessions flow.
type sessRenderer struct {
	acts  []ui3270.ListAction
	views []ui3270.SnapshotView
}

func (r *sessRenderer) List(ui3270.ListView) (ui3270.ListAction, error) { panic("unused") }
func (r *sessRenderer) Form(ui3270.FormView) (ui3270.FormAction, error) { panic("unused") }
func (r *sessRenderer) Detail(ui3270.DetailView) error { return nil }
func (r *sessRenderer) DetailAct(ui3270.DetailView, int) (ui3270.ListAction, error) {
	panic("unexpected DetailAct call") // replaced by a scripting impl in the S-detail task
}
func (r *sessRenderer) Snapshot(v ui3270.SnapshotView) (ui3270.ListAction, error) {
	r.views = append(r.views, v)
	a := r.acts[0]
	r.acts = r.acts[1:]
	return a, nil
}

func newSessionsFlow(reg SessionRegistry, selfID uint64, r ui3270.Renderer, audit *[]store.AuditEvent) *adminFlow {
	return &adminFlow{
		term:          Term{Rows: 24, Cols: 80},
		renderer:      func(_ net.Conn) ui3270.Renderer { return r },
		sessions:      reg,
		selfSessionID: selfID,
		now:           func() time.Time { return time.Unix(1_000_000, 0).UTC() },
		audit:         func(_ context.Context, ev store.AuditEvent) { *audit = append(*audit, ev) },
	}
}

func TestActiveSessions_DisconnectAuditsSubject(t *testing.T) {
	reg := &fakeRegistry{
		views: []SessionView{
			{ID: 1, RemoteAddr: "10.0.0.9:5050", ConnectedAt: time.Unix(999_000, 0), LoggedInAt: time.Unix(999_500, 0), Username: "BOB"},
			{ID: 7, RemoteAddr: "10.0.0.4:6060", ConnectedAt: time.Unix(998_000, 0)}, // pre-auth
		},
		disconnects: map[uint64]SessionView{1: {ID: 1, RemoteAddr: "10.0.0.9:5050", Username: "BOB"}},
		okFor:       map[uint64]bool{1: true},
	}
	var audits []store.AuditEvent
	r := &sessRenderer{acts: []ui3270.ListAction{{Cmd: 'D', Row: 0}, {Cmd: 'D', Row: 0}, {PF: 3}}}
	f := newSessionsFlow(reg, 7 /*self is id 7*/, r, &audits)

	if err := f.activeSessions(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(reg.disconnect) != 1 || reg.disconnect[0] != 1 {
		t.Fatalf("Disconnect calls = %v, want [1]", reg.disconnect)
	}
	if len(audits) != 1 || audits[0].Kind != store.AuditSessionDisconnect || audits[0].Username != "BOB" {
		t.Fatalf("audit = %+v, want one session_disconnect for BOB", audits)
	}
}

func TestActiveSessions_SelfDisconnectVetoed(t *testing.T) {
	reg := &fakeRegistry{
		views:       []SessionView{{ID: 7, RemoteAddr: "10.0.0.4:6060", LoggedInAt: time.Unix(1, 0), Username: "ADMIN"}},
		disconnects: map[uint64]SessionView{},
		okFor:       map[uint64]bool{},
	}
	var audits []store.AuditEvent
	r := &sessRenderer{acts: []ui3270.ListAction{{Cmd: 'D', Row: 0}, {Cmd: 'D', Row: 0}, {PF: 3}}}
	f := newSessionsFlow(reg, 7, r, &audits)

	if err := f.activeSessions(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(reg.disconnect) != 0 {
		t.Errorf("self-session must not be disconnected; got calls %v", reg.disconnect)
	}
	if len(audits) != 0 {
		t.Errorf("no audit on a vetoed self-disconnect; got %+v", audits)
	}
	if r.views[1].ErrMsg != "CANNOT DISCONNECT YOUR OWN SESSION" {
		t.Errorf("veto message = %q", r.views[1].ErrMsg)
	}
}

func TestActiveSessions_RowFormatting(t *testing.T) {
	reg := &fakeRegistry{
		views: []SessionView{
			{ID: 1, RemoteAddr: "10.0.0.9:5050", ConnectedAt: time.Unix(999_000, 0), LoggedInAt: time.Unix(999_500, 0), Username: "BOB", Service: "PROD"},
			{ID: 2, RemoteAddr: "10.0.0.8:5051", ConnectedAt: time.Unix(999_900, 0)}, // pre-auth, no service
		},
	}
	var audits []store.AuditEvent
	r := &sessRenderer{acts: []ui3270.ListAction{{PF: 3}}}
	f := newSessionsFlow(reg, 99 /*self not present*/, r, &audits)
	if err := f.activeSessions(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	row0 := r.views[0].Rows[0].Left
	row1 := r.views[0].Rows[1].Left
	if !strings.Contains(row0, "BOB") || !strings.Contains(row0, "PROD") || !strings.Contains(row0, "10.0.0.9:5050") {
		t.Errorf("row0 = %q, want BOB/PROD/addr", row0)
	}
	if !strings.Contains(row1, "(login)") || !strings.Contains(row1, "-") {
		t.Errorf("row1 = %q, want (login) and '-' service", row1)
	}
	// SESSION is the elapsed clock now-ConnectedAt: 1_000_000-999_000 = 1000s = 00:16:40.
	if !strings.Contains(row0, "00:16:40") {
		t.Errorf("row0 SESSION = %q, want elapsed 00:16:40", row0)
	}
}

// TestFmtSessionRow_RowWidthBounded asserts the packed row never exceeds the
// 72-char content budget (cols 8–79), even for an extreme session id and a long
// IPv6 client address — the id is a process-lifetime monotonic serial, NOT
// bounded by max_conns, so a multi-digit id must clip rather than shift columns.
func TestFmtSessionRow_RowWidthBounded(t *testing.T) {
	now := time.Unix(2_000_000, 0)
	cases := []SessionView{
		{ID: 7, RemoteAddr: "10.0.0.9:5050", ConnectedAt: time.Unix(1_999_000, 0), LoggedInAt: time.Unix(1, 0), Username: "BOB", Service: "PROD"},
		{ID: 123456789, RemoteAddr: "[2001:db8:85a3:8d3:1319:8a2e:370:7348]:65535", ConnectedAt: time.Unix(1_000_000, 0), LoggedInAt: time.Unix(1, 0), Username: "VERYLONGNAME", Service: "SERVICELONG"},
	}
	for _, v := range cases {
		row := fmtSessionRow(v, now, 0).Left
		if n := len([]rune(row)); n > 72 {
			t.Errorf("row width = %d (>72) for id=%d: %q", n, v.ID, row)
		}
	}
	// The 9-digit id (123456789) exceeds the 7-char ID column and must clip with '>'.
	big := fmtSessionRow(cases[1], now, 0).Left
	if !strings.HasPrefix(big, "123456>") {
		t.Errorf("large id not clipped to 7 chars with '>': %q", big)
	}
}

// TestActiveSessions_DisconnectAlreadyGone covers the race where the target
// session ends naturally between the snapshot and the D-confirm: Disconnect
// returns ok=false, so no audit is emitted and the message line says so.
func TestActiveSessions_DisconnectAlreadyGone(t *testing.T) {
	reg := &fakeRegistry{
		views:       []SessionView{{ID: 5, RemoteAddr: "1.2.3.4:9999", LoggedInAt: time.Unix(1, 0), Username: "GONE"}},
		disconnects: map[uint64]SessionView{5: {}},
		okFor:       map[uint64]bool{5: false}, // already ended
	}
	var audits []store.AuditEvent
	r := &sessRenderer{acts: []ui3270.ListAction{{Cmd: 'D', Row: 0}, {Cmd: 'D', Row: 0}, {PF: 3}}}
	f := newSessionsFlow(reg, 99, r, &audits)
	if err := f.activeSessions(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(reg.disconnect) != 1 || reg.disconnect[0] != 5 {
		t.Fatalf("Disconnect calls = %v, want [5]", reg.disconnect)
	}
	if len(audits) != 0 {
		t.Errorf("no audit expected for an already-ended session; got %+v", audits)
	}
	if r.views[2].ErrMsg != "SESSION ALREADY ENDED" {
		t.Errorf("error message = %q, want SESSION ALREADY ENDED", r.views[2].ErrMsg)
	}
}
