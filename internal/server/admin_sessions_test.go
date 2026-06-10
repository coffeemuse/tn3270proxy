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

	"github.com/coffeemuse/tn3270proxy/internal/store"
	"github.com/coffeemuse/tn3270proxy/internal/sysconfig"
	"github.com/coffeemuse/tn3270proxy/internal/ui3270"
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

// sessRenderer scripts Snapshot + DetailAct actions for the activeSessions flow.
type sessRenderer struct {
	acts    []ui3270.ListAction
	views   []ui3270.SnapshotView
	detActs []ui3270.ListAction
	dets    []ui3270.DetailView
}

func (r *sessRenderer) List(ui3270.ListView) (ui3270.ListAction, error) { panic("unused") }
func (r *sessRenderer) Form(ui3270.FormView) (ui3270.FormAction, error) { panic("unused") }
func (r *sessRenderer) Detail(ui3270.DetailView) error                  { return nil }
func (r *sessRenderer) DetailAct(v ui3270.DetailView, _ int) (ui3270.ListAction, error) {
	r.dets = append(r.dets, v)
	if len(r.detActs) == 0 {
		panic("unexpected DetailAct call")
	}
	a := r.detActs[0]
	r.detActs = r.detActs[1:]
	return a, nil
}
func (r *sessRenderer) Snapshot(v ui3270.SnapshotView) (ui3270.ListAction, error) {
	r.views = append(r.views, v)
	a := r.acts[0]
	r.acts = r.acts[1:]
	return a, nil
}

func newSessionsFlow(t *testing.T, reg SessionRegistry, selfID uint64, r ui3270.Renderer, audit *[]store.AuditEvent) *adminFlow {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.SetConfig(context.Background(), sysconfig.KeyAuditReverseDNS, "N"); err != nil {
		t.Fatal(err)
	}
	return &adminFlow{
		store:         st,
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
		},
		disconnects: map[uint64]SessionView{1: {ID: 1, RemoteAddr: "10.0.0.9:5050", Username: "BOB"}},
		okFor:       map[uint64]bool{1: true},
	}
	var audits []store.AuditEvent
	r := &sessRenderer{
		acts:    []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}},
		detActs: []ui3270.ListAction{{PF: 11}, {PF: 11}, {PF: 3}},
	}
	f := newSessionsFlow(t, reg, 7 /*self is id 7, not present*/, r, &audits)

	if err := f.activeSessions(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(reg.disconnect) != 1 || reg.disconnect[0] != 1 {
		t.Fatalf("Disconnect calls = %v, want [1]", reg.disconnect)
	}
	if len(audits) != 1 || audits[0].Kind != store.AuditSessionDisconnect || audits[0].Username != "BOB" {
		t.Fatalf("audit = %+v, want one session_disconnect for BOB", audits)
	}
	if r.dets[1].Message != "CONFIRM DISCONNECT BOB - PRESS PF11 AGAIN" {
		t.Errorf("confirm prompt = %q", r.dets[1].Message)
	}
	if r.dets[2].Message != "DISCONNECTED" {
		t.Errorf("status = %q, want DISCONNECTED", r.dets[2].Message)
	}
}

func TestActiveSessions_SelfDisconnectVetoed(t *testing.T) {
	reg := &fakeRegistry{
		views:       []SessionView{{ID: 7, RemoteAddr: "10.0.0.4:6060", LoggedInAt: time.Unix(1, 0), Username: "ADMIN"}},
		disconnects: map[uint64]SessionView{},
		okFor:       map[uint64]bool{},
	}
	var audits []store.AuditEvent
	r := &sessRenderer{
		acts:    []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}},
		detActs: []ui3270.ListAction{{PF: 11}, {PF: 3}}, // PF11 vetoed, then back
	}
	f := newSessionsFlow(t, reg, 7, r, &audits)

	if err := f.activeSessions(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(reg.disconnect) != 0 {
		t.Errorf("self-session must not be disconnected; got calls %v", reg.disconnect)
	}
	if len(audits) != 0 {
		t.Errorf("no audit on a vetoed self-disconnect; got %+v", audits)
	}
	if r.dets[1].Message != "CANNOT DISCONNECT YOUR OWN SESSION" {
		t.Errorf("veto message = %q", r.dets[1].Message)
	}
}

func TestActiveSessions_DisconnectAlreadyGone(t *testing.T) {
	reg := &fakeRegistry{
		views:       []SessionView{{ID: 5, RemoteAddr: "1.2.3.4:9999", LoggedInAt: time.Unix(1, 0), Username: "GONE"}},
		disconnects: map[uint64]SessionView{5: {}},
		okFor:       map[uint64]bool{5: false}, // already ended
	}
	var audits []store.AuditEvent
	r := &sessRenderer{
		acts:    []ui3270.ListAction{{Cmd: 'S', Row: 0}, {PF: 3}},
		detActs: []ui3270.ListAction{{PF: 11}, {PF: 11}, {PF: 3}},
	}
	f := newSessionsFlow(t, reg, 99, r, &audits)
	if err := f.activeSessions(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(reg.disconnect) != 1 || reg.disconnect[0] != 5 {
		t.Fatalf("Disconnect calls = %v, want [5]", reg.disconnect)
	}
	if len(audits) != 0 {
		t.Errorf("no audit expected for an already-ended session; got %+v", audits)
	}
	if r.dets[1].Message != "CONFIRM DISCONNECT GONE - PRESS PF11 AGAIN" {
		t.Errorf("confirm prompt = %q", r.dets[1].Message)
	}
	if r.dets[2].Message != "SESSION ALREADY ENDED" {
		t.Errorf("status = %q, want SESSION ALREADY ENDED", r.dets[2].Message)
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
	f := newSessionsFlow(t, reg, 99 /*self not present*/, r, &audits)
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

func TestSessionDetail_Fields(t *testing.T) {
	r := &sessRenderer{}
	var audits []store.AuditEvent
	f := newSessionsFlow(t, &fakeRegistry{}, 0, r, &audits) // reverse DNS off
	v := SessionView{
		ID: 14, RemoteAddr: "[2001:db8:85a3:8d3:1319:8a2e:370:7348]:65535",
		ConnectedAt: time.Unix(999_000, 0), LoggedInAt: time.Unix(999_500, 0),
		Username: "DARROW", Service: "DEMO",
	}
	dv := f.sessionDetail(context.Background(), v)

	want := map[string]string{
		"Session": "14",
		"Client":  "[2001:db8:85a3:8d3:1319:8a2e:370:7348]:65535",
		"User":    "DARROW",
		"Service": "DEMO",
	}
	got := map[string]string{}
	for _, fld := range dv.Fields {
		got[fld.Label] = fld.Value
	}
	for label, w := range want {
		if got[label] != w {
			t.Errorf("field %q = %q, want %q", label, got[label], w)
		}
	}
	if _, ok := got["PTR"]; ok {
		t.Errorf("PTR must be absent when reverse DNS is off; fields=%+v", dv.Fields)
	}
	if !strings.Contains(got["Connected"], "00:16:40") { // 1_000_000-999_000 = 1000s
		t.Errorf("Connected = %q, want elapsed 00:16:40", got["Connected"])
	}
	if !strings.Contains(got["Logged in"], "00:08:20") { // 1_000_000-999_500 = 500s
		t.Errorf("Logged in = %q, want elapsed 00:08:20", got["Logged in"])
	}
	if dv.PFHelp != "PF3=Back   PF11=Disconnect" {
		t.Errorf("PFHelp = %q", dv.PFHelp)
	}
}

func TestSessionDetail_PreAuthAndUnbridged(t *testing.T) {
	r := &sessRenderer{}
	var audits []store.AuditEvent
	f := newSessionsFlow(t, &fakeRegistry{}, 0, r, &audits)
	v := SessionView{ID: 2, RemoteAddr: "10.0.0.8:5051", ConnectedAt: time.Unix(999_900, 0)} // pre-auth, no service
	dv := f.sessionDetail(context.Background(), v)
	got := map[string]string{}
	for _, fld := range dv.Fields {
		got[fld.Label] = fld.Value
	}
	if got["User"] != "(login)" {
		t.Errorf("User = %q, want (login)", got["User"])
	}
	if got["Service"] != "-" {
		t.Errorf("Service = %q, want -", got["Service"])
	}
	if got["Logged in"] != "(not logged in)" {
		t.Errorf("Logged in = %q, want (not logged in)", got["Logged in"])
	}
}

func TestSessionDetail_PTRWhenEnabled(t *testing.T) {
	r := &sessRenderer{}
	var audits []store.AuditEvent
	f := newSessionsFlow(t, &fakeRegistry{}, 0, r, &audits)
	if err := f.store.(*store.Store).SetConfig(context.Background(), sysconfig.KeyAuditReverseDNS, "Y"); err != nil {
		t.Fatal(err)
	}
	f.resolver = fakeResolver{names: []string{"host.example.de."}}
	v := SessionView{ID: 3, RemoteAddr: "203.0.113.9:5050", ConnectedAt: time.Unix(999_000, 0)}
	dv := f.sessionDetail(context.Background(), v)
	var ptrVal string
	for _, fld := range dv.Fields {
		if fld.Label == "PTR" {
			ptrVal = fld.Value
		}
	}
	if ptrVal != "host.example.de" {
		t.Errorf("PTR = %q, want host.example.de", ptrVal)
	}
}
