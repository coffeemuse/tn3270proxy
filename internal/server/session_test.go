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
	"errors"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/bridge"
	"github.com/CoffeeMuse/tn3270proxy/internal/mfa"
	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/CoffeeMuse/tn3270proxy/internal/ui3270"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
)

// --- fakes ---

type fakePresenter struct {
	termType          string
	rows, cols        int   // 0,0 → Negotiate reports 24×80
	negErr            error // non-nil → Negotiate fails
	logins            []loginResult
	menuPicks         []menuResult
	menuErrors        []string
	loginErrors       []string
	gotAdminFlag      []bool
	gotSettingsLocked []bool
	gotTerms          []Term               // every term passed to Login/Menu, in call order
	gotStatus         []screens.MenuStatus // every status passed to Menu, in call order
	loginStatuses     []screens.MenuStatus // every status passed to Login, in call order
	newsCalls         [][][]string         // pages passed to each News call, in order
	newsResults       []error              // queued News return values; default nil
	enrolls           []mfaResult
	verifies          []mfaResult
	enrollErrors      []string // errMsg passed to each EnrollMFA call
	verifyErrors      []string // errMsg passed to each VerifyMFA call
	gotChunked        []string // chunkedSecret passed to each EnrollMFA call
	userSettingsCalls int
	userSettingsPicks []userSettingsResult
}

type loginResult struct {
	user, pass string
	quit       bool
	err        error
}
type mfaResult struct {
	code string
	quit bool
	err  error
}
type menuResult struct {
	sel    *store.Service
	choice menuChoice
	quit   bool // convenience: when true, choice is forced to menuQuit
	err    error
}
type userSettingsResult struct {
	choice string
	back   bool
	err    error
}

func (f *fakePresenter) Negotiate(conn net.Conn) (Term, error) {
	if f.negErr != nil {
		return Term{}, f.negErr
	}
	rows, cols := f.rows, f.cols
	if rows == 0 {
		rows, cols = 24, 80
	}
	return Term{Type: f.termType, Rows: rows, Cols: cols}, nil
}

func (f *fakePresenter) Login(conn net.Conn, term Term, status screens.MenuStatus, errMsg string) (string, string, bool, error) {
	f.gotTerms = append(f.gotTerms, term)
	f.loginStatuses = append(f.loginStatuses, status)
	f.loginErrors = append(f.loginErrors, errMsg)
	r := f.logins[0]
	f.logins = f.logins[1:]
	return r.user, r.pass, r.quit, r.err
}

func (f *fakePresenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, settingsLocked bool, status screens.MenuStatus, errMsg string) (*store.Service, menuChoice, error) {
	f.gotTerms = append(f.gotTerms, term)
	f.menuErrors = append(f.menuErrors, errMsg)
	f.gotAdminFlag = append(f.gotAdminFlag, admin)
	f.gotSettingsLocked = append(f.gotSettingsLocked, settingsLocked)
	f.gotStatus = append(f.gotStatus, status)
	r := f.menuPicks[0]
	f.menuPicks = f.menuPicks[1:]
	ch := r.choice
	if r.quit {
		ch = menuQuit
	}
	return r.sel, ch, r.err
}

func (f *fakePresenter) UserSettings(conn net.Conn, term Term, username string, rows []screens.UserSettingsRow, errMsg string) (string, bool, error) {
	f.userSettingsCalls++
	if len(f.userSettingsPicks) > 0 {
		r := f.userSettingsPicks[0]
		f.userSettingsPicks = f.userSettingsPicks[1:]
		return r.choice, r.back, r.err
	}
	return "", true, nil // default: PF3 back to the service menu
}

func (f *fakePresenter) News(conn net.Conn, term Term, pages [][]string) error {
	f.gotTerms = append(f.gotTerms, term)
	f.newsCalls = append(f.newsCalls, pages)
	if len(f.newsResults) > 0 {
		r := f.newsResults[0]
		f.newsResults = f.newsResults[1:]
		return r
	}
	return nil
}

func (f *fakePresenter) EnrollMFA(conn net.Conn, term Term, issuer, account, chunkedSecret, errMsg string) (string, bool, error) {
	f.enrollErrors = append(f.enrollErrors, errMsg)
	f.gotChunked = append(f.gotChunked, chunkedSecret)
	r := f.enrolls[0]
	f.enrolls = f.enrolls[1:]
	return r.code, r.quit, r.err
}

func (f *fakePresenter) VerifyMFA(conn net.Conn, term Term, errMsg string) (string, bool, error) {
	f.verifyErrors = append(f.verifyErrors, errMsg)
	r := f.verifies[0]
	f.verifies = f.verifies[1:]
	return r.code, r.quit, r.err
}

type fakeBridger struct {
	causes []bridge.Cause
	errs   []error
	calls  int
	gotTLS []BackendTLS
}

func (f *fakeBridger) Bridge(conn net.Conn, addr, termType string, escapeAID byte, btls BackendTLS) (bridge.Cause, error) {
	f.gotTLS = append(f.gotTLS, btls)
	i := f.calls
	f.calls++
	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}
	return f.causes[i], err
}

func authStub(ctx context.Context, st auth.UserStore, user, pass string) (auth.Identity, error) {
	if user == "alice" && pass == "good" {
		return auth.Identity{UserID: 1, Username: "alice", Groups: []string{"ops"}}, nil
	}
	if user == "root" && pass == "good" {
		return auth.Identity{UserID: 9, Username: "root", Groups: []string{store.AdminGroup}}, nil
	}
	return auth.Identity{}, auth.ErrInvalidCredentials
}

func newTestSession(t *testing.T, p *fakePresenter, b *fakeBridger) *Session {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	gid, _ := st.CreateGroup(ctx, "ops")
	sid, _ := st.CreateService(ctx, "PROD", "Production", "10.0.0.1", 23, false, true)
	st.LinkGroupService(ctx, gid, sid)
	// Create "alice" and "root" so GetUserByUsername in the menu lock-load always
	// succeeds for both authStub users. The stored password hashes here are never
	// bcrypt-checked in tests that use authStub; tests using auth.Authenticate
	// (usersettings_test.go) overwrite alice's hash via SetPassword.
	aliceHash, err := auth.HashPassword("good")
	if err != nil {
		t.Fatal(err)
	}
	st.CreateUser(ctx, "alice", aliceHash)
	st.CreateUser(ctx, "root", "x") // authStub users; hash not bcrypt-verified here

	return &Session{
		Store:        st,
		Authenticate: authStub,
		Presenter:    p,
		Bridger:      b,
		EscapeAID:    0x6B,
	}
}

func TestSessionLoginRetryThenQuit(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "bad"},
			{user: "alice", pass: "good"},
			{quit: true}, // second login render after menu logoff
		},
		menuPicks: []menuResult{{quit: true}},
	}
	b := &fakeBridger{}
	s := newTestSession(t, p, b)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.logins) != 0 {
		t.Errorf("expected both login attempts consumed, %d left", len(p.logins))
	}
	if b.calls != 0 {
		t.Errorf("bridge should not be called when user quits at menu")
	}
	// First login render has no error; the retry after bad creds must carry a
	// generic (non-empty) message — and must not leak whether the user exists.
	if len(p.loginErrors) < 2 {
		t.Fatalf("expected at least 2 login renders, got %d", len(p.loginErrors))
	}
	if p.loginErrors[0] != "" {
		t.Errorf("first login render errMsg = %q, want empty", p.loginErrors[0])
	}
	if p.loginErrors[1] == "" {
		t.Errorf("retry login render should show a generic auth-failure message")
	}
}

func TestSessionEscapeReturnsToMenu(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // second login render after menu logoff
		},
		menuPicks: []menuResult{
			{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}, choice: menuService},
			{quit: true},
		},
	}
	b := &fakeBridger{causes: []bridge.Cause{bridge.CauseUserEscaped}}
	s := newTestSession(t, p, b)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if b.calls != 1 {
		t.Errorf("bridge calls = %d, want 1", b.calls)
	}
	if len(p.menuPicks) != 0 {
		t.Errorf("expected to return to menu after escape; %d picks left", len(p.menuPicks))
	}
}

func TestSessionBackendErrorShownOnMenu(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // second login render after menu logoff
		},
		menuPicks: []menuResult{
			{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}, choice: menuService},
			{quit: true},
		},
	}
	b := &fakeBridger{
		causes: []bridge.Cause{bridge.CauseError},
		errs:   []error{errors.New("connection refused")},
	}
	s := newTestSession(t, p, b)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.menuErrors) < 2 || p.menuErrors[1] != "Could not connect to PROD" {
		t.Errorf("expected connect-failure message on menu after backend failure; got %v", p.menuErrors)
	}
}

func TestSessionPassesTLSIntentToBridger(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // second login render after menu logoff
		},
		menuPicks: []menuResult{
			{sel: &store.Service{Name: "SEC", Host: "10.0.0.9", Port: 992, TLS: true, TLSVerify: true}, choice: menuService},
			{quit: true},
		},
	}
	b := &fakeBridger{causes: []bridge.Cause{bridge.CauseUserEscaped}}
	s := newTestSession(t, p, b)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(b.gotTLS) != 1 {
		t.Fatalf("bridge called %d times, want 1", len(b.gotTLS))
	}
	if got := b.gotTLS[0]; !got.Enabled || !got.Verify {
		t.Errorf("intent = %+v, want {Enabled:true Verify:true}", got)
	}
}

func TestSessionClientClosedEndsSession(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{user: "alice", pass: "good"}},
		menuPicks: []menuResult{
			{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}, choice: menuService},
		},
	}
	b := &fakeBridger{causes: []bridge.Cause{bridge.CauseClientClosed}}
	s := newTestSession(t, p, b)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if b.calls != 1 {
		t.Errorf("bridge calls = %d, want 1", b.calls)
	}
}

func TestSessionAdminFlagFollowsGroup(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"}, // groups: ops
			{quit: true},                  // second login render after menu logoff
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.AdminPresenter = &fakeAdminPresenter{}
	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)
	if len(p.gotAdminFlag) != 1 || p.gotAdminFlag[0] {
		t.Errorf("admin flag for non-admin = %v, want [false]", p.gotAdminFlag)
	}
}

func TestSessionAdminSelectionRunsFlowAndReturnsToMenu(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "root", pass: "good"}, // groups: ZZADMIN
			{quit: true},                 // second login render after menu logoff
		},
		menuPicks: []menuResult{{choice: menuAdmin}, {quit: true}},
	}
	ap := &fakeAdminPresenter{menu: []adminMenuStep{{back: true}}}
	s := newTestSession(t, p, &fakeBridger{})
	s.AdminPresenter = ap
	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)
	if len(ap.menu) != 0 {
		t.Errorf("admin flow was not run")
	}
	if len(p.gotAdminFlag) != 2 || !p.gotAdminFlag[0] {
		t.Errorf("admin flag = %v, want [true true]", p.gotAdminFlag)
	}
	if len(p.menuPicks) != 0 {
		t.Errorf("expected return to menu after admin flow; %d picks left", len(p.menuPicks))
	}
}

func TestSessionUserSettingsReturnsToMenu(t *testing.T) {
	// Non-admin user selects "0" (user settings), the flow runs and returns
	// (back=true), then the user logs off. Verifies end-to-end dispatch wiring.
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"}, // groups: ops (non-admin)
			{quit: true},                  // second login render after menu logoff
		},
		menuPicks: []menuResult{
			{choice: menuUserSettings}, // select "0"
			{quit: true},               // log off after returning to menu
		},
		// userSettingsPicks is empty: default back=true fires immediately
	}
	s := newTestSession(t, p, &fakeBridger{})
	// userSettings calls GetUserByUsername, so the user must exist in the store.
	if _, err := s.Store.CreateUser(context.Background(), "alice", "good"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	// UserSettings must have been called exactly once.
	if p.userSettingsCalls != 1 {
		t.Errorf("userSettingsCalls = %d, want 1", p.userSettingsCalls)
	}
	// Both menu picks must have been consumed (returned to menu, then logged off).
	if len(p.menuPicks) != 0 {
		t.Errorf("expected return to menu after user settings; %d picks left", len(p.menuPicks))
	}
	// A logout event with "user logoff" must have been recorded.
	var logoutEv *store.AuditEvent
	for i := range rec.events {
		if rec.events[i].Kind == store.AuditLogout {
			logoutEv = &rec.events[i]
			break
		}
	}
	if logoutEv == nil {
		t.Fatalf("no logout event recorded; kinds = %v", rec.kinds())
	}
	if logoutEv.Detail != "user logoff" {
		t.Errorf("logout detail = %q, want %q", logoutEv.Detail, "user logoff")
	}
}

func TestSessionNonAdminAdminSelIgnored(t *testing.T) {
	// Even if a (buggy) presenter reports adminSel=true for a non-admin,
	// the session's isAdmin double-guard must not run the admin flow.
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"}, // ops, not ZZADMIN
			{quit: true},                  // second login render after menu logoff
		},
		menuPicks: []menuResult{{choice: menuAdmin}, {quit: true}},
	}
	ap := &fakeAdminPresenter{} // no scripted steps: any call would panic
	s := newTestSession(t, p, &fakeBridger{})
	s.AdminPresenter = ap
	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)
	if len(ap.gotMenuErrs) != 0 {
		t.Errorf("admin flow ran for non-admin user")
	}
}

func TestSessionLockedSettingsGuard(t *testing.T) {
	// Even if a (buggy) presenter reports menuUserSettings for a locked user,
	// the session's settingsLocked guard must not run the user-settings flow.
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"}, // alice exists; lock is set below
			{quit: true},                  // second login render after menu logoff
		},
		menuPicks: []menuResult{{choice: menuUserSettings}, {quit: true}},
		// userSettingsPicks is empty: any real call would use the default back path
	}
	s := newTestSession(t, p, &fakeBridger{})
	ctx := context.Background()
	// alice is already in the store (created by newTestSession); look up her ID
	// and set the lock so the menu-loop snapshot captures it.
	u, err := s.Store.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if err := s.Store.SetUserSettingsLocked(ctx, u.ID, true); err != nil {
		t.Fatalf("SetUserSettingsLocked: %v", err)
	}

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	// The guard must have blocked the flow: userSettings must NOT have been called.
	if p.userSettingsCalls != 0 {
		t.Errorf("userSettingsCalls = %d, want 0 (locked user must not enter settings)", p.userSettingsCalls)
	}
	// Both menu picks must have been consumed (re-prompted after the guard, then logged off).
	if len(p.menuPicks) != 0 {
		t.Errorf("expected both menu picks consumed; %d left", len(p.menuPicks))
	}
	// The flag must have propagated into the Menu call.
	if len(p.gotSettingsLocked) == 0 || !p.gotSettingsLocked[0] {
		t.Errorf("gotSettingsLocked = %v, want [true ...]", p.gotSettingsLocked)
	}
}

func TestSessionMenuQuitLogsOffToLogin(t *testing.T) {
	// PF3 at the menu logs off (back to login); PF3 at login disconnects.
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // second login render: user disconnects
		},
		menuPicks: []menuResult{{quit: true}},
	}
	b := &fakeBridger{}
	s := newTestSession(t, p, b)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.logins) != 0 {
		t.Errorf("menu quit should re-render login; %d logins left", len(p.logins))
	}
	// both login renders are pristine (no logoff notice — spec decision)
	if len(p.loginErrors) != 2 || p.loginErrors[0] != "" || p.loginErrors[1] != "" {
		t.Errorf("login renders = %q, want two empty messages", p.loginErrors)
	}
	if b.calls != 0 {
		t.Errorf("bridge calls = %d, want 0", b.calls)
	}
}

func TestSessionReloginRecomputesAdmin(t *testing.T) {
	// root (ZZADMIN) logs off; alice (ops) logs in: the A entry must vanish.
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "root", pass: "good"},
			{user: "alice", pass: "good"},
			{quit: true},
		},
		menuPicks: []menuResult{{quit: true}, {quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.AdminPresenter = &fakeAdminPresenter{}
	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)
	if len(p.gotAdminFlag) != 2 || !p.gotAdminFlag[0] || p.gotAdminFlag[1] {
		t.Errorf("admin flags = %v, want [true false]", p.gotAdminFlag)
	}
}

func TestSessionThreadsTermToScreens(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-4",
		rows:      43,
		cols:      80,
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)
	if len(p.gotTerms) == 0 {
		t.Fatal("no terms captured")
	}
	for i, term := range p.gotTerms {
		if term.Type != "IBM-3278-4" || term.Rows != 43 || term.Cols != 80 {
			t.Errorf("call %d: term = %+v, want IBM-3278-4 43x80", i, term)
		}
	}
}

// recordingAuditor captures every audit event for sequence assertions.
type recordingAuditor struct {
	events []store.AuditEvent
}

func (r *recordingAuditor) Record(_ context.Context, ev store.AuditEvent) {
	r.events = append(r.events, ev)
}

func (r *recordingAuditor) kinds() []string {
	out := make([]string, len(r.events))
	for i, ev := range r.events {
		out[i] = ev.Kind
	}
	return out
}

func TestSessionAuditsConnectAndDisconnect(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{quit: true}}, // user quits at login
	}
	s := newTestSession(t, p, &fakeBridger{})
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	kinds := rec.kinds()
	if len(kinds) != 2 || kinds[0] != store.AuditConnect || kinds[1] != store.AuditDisconnect {
		t.Fatalf("kinds = %v, want [connect disconnect]", kinds)
	}
	for _, ev := range rec.events {
		if len(ev.SessionID) != 16 {
			t.Errorf("%s: session id %q, want 16 hex chars", ev.Kind, ev.SessionID)
		}
		if ev.RemoteAddr == "" {
			t.Errorf("%s: empty remote addr", ev.Kind)
		}
	}
	if rec.events[0].SessionID != rec.events[1].SessionID {
		t.Error("session ids differ within one connection")
	}
	disc := rec.events[1]
	if disc.Detail != "quit at login" {
		t.Errorf("disconnect detail = %q, want %q", disc.Detail, "quit at login")
	}
	if disc.Username != "" {
		t.Errorf("disconnect username = %q, want empty (quit before login)", disc.Username)
	}
}

func TestSessionAuditsDisconnectAfterClientClosed(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}},
		menuPicks: []menuResult{{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}, choice: menuService}},
	}
	b := &fakeBridger{causes: []bridge.Cause{bridge.CauseClientClosed}}
	s := newTestSession(t, p, b)
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if b.calls != 1 {
		t.Errorf("bridge calls = %d, want 1", b.calls)
	}
	kinds := rec.kinds()
	if len(kinds) == 0 || kinds[len(kinds)-1] != store.AuditDisconnect {
		t.Fatalf("last event kind = %v, want disconnect", kinds)
	}
	disc := rec.events[len(rec.events)-1]
	if disc.Detail != "client closed during bridge" {
		t.Errorf("disconnect detail = %q, want %q", disc.Detail, "client closed during bridge")
	}
	if disc.Username != "alice" {
		t.Errorf("disconnect username = %q, want %q", disc.Username, "alice")
	}
	// A mid-session client drop is attributed to the still-logged-in actor (#73):
	// doLogin's setActor("") only fires on return-to-login, which never happened here.
	if disc.Actor != "alice" {
		t.Errorf("disconnect actor = %q, want %q (still logged in)", disc.Actor, "alice")
	}
}

func TestSessionAuditsAuthEvents(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "sw0rdf1sh-wrong"},
			{user: "alice", pass: "good"},
			{quit: true}, // second login render after menu logoff
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	want := []string{store.AuditConnect, store.AuditAuthFail, store.AuditAuthOK, store.AuditLogout, store.AuditDisconnect}
	if !slices.Equal(rec.kinds(), want) {
		t.Fatalf("kinds = %v, want %v", rec.kinds(), want)
	}
	fail := rec.events[1]
	if fail.Username != "alice" {
		t.Errorf("auth_fail username = %q, want the attempted username", fail.Username)
	}
	// The password must not appear in ANY field of ANY event.
	for _, ev := range rec.events {
		for _, field := range []string{ev.SessionID, ev.Kind, ev.Username, ev.Actor, ev.RemoteAddr, ev.Service, ev.Detail} {
			if strings.Contains(field, "sw0rdf1sh-wrong") || strings.Contains(field, "good") {
				t.Errorf("credential leaked into audit event %+v", ev)
			}
		}
	}
}

func TestSessionAuditsBridgeLifecycle(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{
			{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}, choice: menuService},
			{quit: true},
		},
	}
	b := &fakeBridger{causes: []bridge.Cause{bridge.CauseUserEscaped}}
	s := newTestSession(t, p, b)
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	want := []string{store.AuditConnect, store.AuditAuthOK,
		store.AuditBridgeStart, store.AuditBridgeEnd, store.AuditLogout, store.AuditDisconnect}
	if !slices.Equal(rec.kinds(), want) {
		t.Fatalf("kinds = %v, want %v", rec.kinds(), want)
	}
	start, end := rec.events[2], rec.events[3]
	if start.Service != "PROD" || start.Username != "alice" {
		t.Errorf("bridge_start = %+v, want service PROD by alice", start)
	}
	if end.Service != "PROD" || end.Detail != "user_escaped" {
		t.Errorf("bridge_end = %+v, want service PROD detail user_escaped", end)
	}
}

func TestSessionBridgeEndDetailOnDialError(t *testing.T) {
	// Verifies that a CauseError bridge outcome with a non-nil error produces
	// a bridge_end audit event whose Detail carries the error string and whose
	// Service matches the selected service name.
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // second login render after menu logoff
		},
		menuPicks: []menuResult{
			{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}, choice: menuService},
			{quit: true},
		},
	}
	b := &fakeBridger{
		causes: []bridge.Cause{bridge.CauseError},
		errs:   []error{errors.New("connection refused")},
	}
	s := newTestSession(t, p, b)
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	var bridgeEnd *store.AuditEvent
	for i := range rec.events {
		if rec.events[i].Kind == store.AuditBridgeEnd {
			bridgeEnd = &rec.events[i]
			break
		}
	}
	if bridgeEnd == nil {
		t.Fatal("no bridge_end event recorded")
	}
	if bridgeEnd.Detail != "error: connection refused" {
		t.Errorf("bridge_end Detail = %q, want %q", bridgeEnd.Detail, "error: connection refused")
	}
	if bridgeEnd.Service != "PROD" {
		t.Errorf("bridge_end Service = %q, want %q", bridgeEnd.Service, "PROD")
	}
}

func TestSessionAuthInfraErrorRepresentsLoginScreen(t *testing.T) {
	// A non-credential error from Authenticate must NOT disconnect the client.
	// The login screen is re-presented with a temporary-error message, an
	// auth_error event is audited with the username and error text, and the
	// session ultimately ends normally (not with "quit at login").
	infraErr := errors.New("database unavailable")
	callCount := 0
	authWithTransient := func(ctx context.Context, st auth.UserStore, user, pass string) (auth.Identity, error) {
		callCount++
		if callCount == 1 {
			return auth.Identity{}, infraErr
		}
		return authStub(ctx, st, user, pass)
	}

	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"}, // first attempt → infra error
			{user: "alice", pass: "good"}, // retry → success
			{quit: true},                  // after menu logoff
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	rec := &recordingAuditor{}
	s.Auditor = rec
	s.Authenticate = authWithTransient

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	// Retry render must carry a temp-error message (not the credential-failure msg).
	if len(p.loginErrors) < 2 {
		t.Fatalf("expected at least 2 login renders, got %d", len(p.loginErrors))
	}
	if p.loginErrors[0] != "" {
		t.Errorf("first login render errMsg = %q, want empty", p.loginErrors[0])
	}
	if p.loginErrors[1] == "" {
		t.Errorf("retry login render should show a temporary-error message")
	}
	if strings.Contains(p.loginErrors[1], "Invalid") {
		t.Errorf("retry message must not reuse the credential-failure text, got %q", p.loginErrors[1])
	}

	// Audit trail must contain an auth_error event.
	kinds := rec.kinds()
	if !slices.Contains(kinds, store.AuditAuthError) {
		t.Errorf("kinds = %v, want an %s event", kinds, store.AuditAuthError)
	}

	// auth_error event must carry the username and error text; never the password.
	var authErrEv *store.AuditEvent
	for i := range rec.events {
		if rec.events[i].Kind == store.AuditAuthError {
			authErrEv = &rec.events[i]
			break
		}
	}
	if authErrEv == nil {
		t.Fatal("no auth_error event recorded")
	}
	if authErrEv.Username != "alice" {
		t.Errorf("auth_error username = %q, want alice", authErrEv.Username)
	}
	if !strings.Contains(authErrEv.Detail, "database unavailable") {
		t.Errorf("auth_error detail = %q, want error text included", authErrEv.Detail)
	}

	// The infra error must not have aborted the session: the retry login
	// succeeds, so an auth_ok event must follow the auth_error (rather than the
	// session disconnecting at the first error, as it did before the fix).
	errIdx := slices.IndexFunc(rec.events, func(e store.AuditEvent) bool {
		return e.Kind == store.AuditAuthError
	})
	okIdx := slices.IndexFunc(rec.events, func(e store.AuditEvent) bool {
		return e.Kind == store.AuditAuthOK
	})
	if okIdx < 0 {
		t.Errorf("kinds = %v, want an %s event after the infra error", kinds, store.AuditAuthOK)
	} else if okIdx < errIdx {
		t.Errorf("auth_ok (idx %d) should follow auth_error (idx %d)", okIdx, errIdx)
	}
}

func TestSessionLoginRenderErrorHasDistinctDetail(t *testing.T) {
	// A Presenter.Login error must yield "login render error" in the disconnect
	// event, not "quit at login".
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{err: errors.New("render failed")}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	disc := rec.events[len(rec.events)-1]
	if disc.Kind != store.AuditDisconnect {
		t.Fatalf("last event kind = %q, want disconnect", disc.Kind)
	}
	if disc.Detail != "login render error" {
		t.Errorf("disconnect detail = %q, want %q", disc.Detail, "login render error")
	}
}

func TestCauseDetail(t *testing.T) {
	cases := []struct {
		c    bridge.Cause
		err  error
		want string
	}{
		{bridge.CauseBackendClosed, nil, "backend_closed"},
		{bridge.CauseClientClosed, nil, "client_closed"},
		{bridge.CauseUserEscaped, nil, "user_escaped"},
		{bridge.CauseError, errors.New("connection refused"), "error: connection refused"},
		{bridge.CauseError, nil, "error"},
	}
	for _, c := range cases {
		if got := causeDetail(c.c, c.err); got != c.want {
			t.Errorf("causeDetail(%v, %v) = %q, want %q", c.c, c.err, got, c.want)
		}
	}
}

func TestSessionThreadsAuditIntoAdminFlow(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "root", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{choice: menuAdmin}, {quit: true}},
	}
	ap := &fakeAdminPresenter{
		menu:  []adminMenuStep{{choice: 2}, {back: true}},
		lists: []ui3270.ListAction{{PF: 4}, {PF: 3}}, // groups list: PF4 add, then back
		forms: []ui3270.FormAction{{Values: map[string]string{screens.FieldName: "newgrp"}}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.AdminPresenter = ap
	s.AdminRenderer = func(net.Conn, Term) ui3270.Renderer { return ap }
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	var admins []store.AuditEvent
	for _, ev := range rec.events {
		if ev.Kind == store.AuditAdmin {
			admins = append(admins, ev)
		}
	}
	// GH #73: generic CRUD subject lives in Detail; Username must be empty; actor
	// is auto-filled by auditTrail.record with the authenticated principal.
	if len(admins) != 1 || admins[0].Detail != "group create newgrp" ||
		admins[0].Username != "" || admins[0].Actor != "root" || admins[0].SessionID == "" {
		t.Errorf("admin events = %+v, want one 'group create newgrp' with actor=root, empty username, and a session id", admins)
	}
}

func TestSessionPopulatesMenuStatus(t *testing.T) {
	// Verify that the session builds screens.MenuStatus with the correct
	// Username (from the identity), SystemID ("PROXY", the seeded sysconfig
	// default), and Release (from Session.Release) before calling Presenter.Menu.
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true}, // second login render after menu logoff
		},
		menuPicks: []menuResult{{quit: true}},
	}
	b := &fakeBridger{}
	s := newTestSession(t, p, b)
	s.Release = "vTEST"

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.gotStatus) == 0 {
		t.Fatal("Menu was never called")
	}
	got := p.gotStatus[0]
	if got.Release != "vTEST" {
		t.Errorf("status.Release = %q, want vTEST", got.Release)
	}
	if got.SystemID != "PROXY" {
		t.Errorf("status.SystemID = %q, want PROXY", got.SystemID)
	}
	// authStub returns Username: "alice" (lowercase) — the store layer would
	// canonicalize to uppercase, but this test exercises the session seam, not
	// the store, so the identity flows through unchanged.
	if got.Username != "alice" {
		t.Errorf("status.Username = %q, want alice", got.Username)
	}

	// The login screen gets the same SystemID/Release for its info block (no
	// Username pre-login).
	if len(p.loginStatuses) == 0 {
		t.Fatal("Login was never called")
	}
	ls := p.loginStatuses[0]
	if ls.Release != "vTEST" || ls.SystemID != "PROXY" {
		t.Errorf("login status = %+v, want Release vTEST / SystemID PROXY", ls)
	}
	if ls.Username != "" {
		t.Errorf("login status Username = %q, want empty pre-login", ls.Username)
	}
}

func TestSessionMenuStatusUsesConfiguredSystemID(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true},
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	if err := s.Store.SetConfig(context.Background(), "SYSTEM_ID", "SYSA"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.gotStatus) == 0 {
		t.Fatal("Menu was never called")
	}
	if got := p.gotStatus[0].SystemID; got != "SYSA" {
		t.Errorf("status.SystemID = %q, want SYSA", got)
	}
}

func TestSessionMenuStatusSystemIDFallback(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"},
			{quit: true},
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	// An empty stored value must fall back to PROXY (menu never renders blank).
	if err := s.Store.SetConfig(context.Background(), "SYSTEM_ID", ""); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.gotStatus) == 0 {
		t.Fatal("Menu was never called")
	}
	if got := p.gotStatus[0].SystemID; got != "PROXY" {
		t.Errorf("status.SystemID = %q, want PROXY (fallback)", got)
	}
}

func newMFATestSession(t *testing.T, p *fakePresenter, b *fakeBridger) (*Session, *store.Store) {
	t.Helper()
	s := newTestSession(t, p, b)
	c, err := mfa.NewCipher(make([]byte, mfa.KeyLen))
	if err != nil {
		t.Fatal(err)
	}
	s.MFA = c
	s.Now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return s, s.Store
}

func codeForServer(t *testing.T, secret string, step uint64) string {
	t.Helper()
	code, err := hotp.GenerateCodeCustom(secret, step, hotp.ValidateOpts{
		Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestMFAEnrollFlow(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP" // fixed 16-char base32
	now := time.Unix(1_700_000_000, 0)
	step := uint64(now.Unix() / 30)
	good := codeForServer(t, secret, step)

	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		enrolls: []mfaResult{
			{code: "000000"}, // wrong first
			{code: good},     // correct
		},
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{quit: true}},
	}
	b := &fakeBridger{}
	s, st := newMFATestSession(t, p, b)
	s.MFAGenerate = func(_, _ string) (string, error) { return secret, nil }
	ctx := context.Background()
	uid, _ := st.CreateUser(ctx, "alice", "x")
	st.SetMFARequired(ctx, uid, true)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.enrolls) != 0 {
		t.Fatalf("expected both enroll attempts consumed, %d left", len(p.enrolls))
	}
	u, _ := st.GetUserByUsername(ctx, "alice")
	if u.MFASecret == "" || u.MFAEnrolledAt == "" {
		t.Fatalf("enrollment should have persisted an encrypted secret: %+v", u)
	}
	pt, err := s.MFA.Open(u.MFASecret)
	if err != nil || string(pt) != secret {
		t.Fatalf("stored secret mismatch: %q err=%v", pt, err)
	}
	if u.MFALastStep != int64(step) {
		t.Fatalf("replay floor not set: %d", u.MFALastStep)
	}
}

func TestMFAVerifyFlow(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_700_000_000, 0)
	step := uint64(now.Unix() / 30)
	good := codeForServer(t, secret, step)

	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		verifies:  []mfaResult{{code: "000000"}, {code: good}}, // wrong then right
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{quit: true}},
	}
	b := &fakeBridger{}
	s, st := newMFATestSession(t, p, b)
	ctx := context.Background()
	uid, _ := st.CreateUser(ctx, "alice", "x")
	st.SetMFARequired(ctx, uid, true)
	enc, _ := s.MFA.Seal([]byte(secret))
	st.StoreMFAEnrollment(ctx, uid, enc, "2026-01-01T00:00:00Z", 0)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(p.verifies) != 0 {
		t.Fatalf("expected both verify attempts consumed, %d left", len(p.verifies))
	}
	u, _ := st.GetUserByUsername(ctx, "alice")
	if u.MFALastStep != int64(step) {
		t.Fatalf("replay floor not advanced: %d", u.MFALastStep)
	}
}

func TestMFAVerifyPF3CancelsToLogin(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		verifies: []mfaResult{{quit: true}}, // PF3 at the code prompt
		logins:   []loginResult{{user: "alice", pass: "good"}, {quit: true}},
	}
	b := &fakeBridger{}
	s, st := newMFATestSession(t, p, b)
	ctx := context.Background()
	uid, _ := st.CreateUser(ctx, "alice", "x")
	st.SetMFARequired(ctx, uid, true)
	enc, _ := s.MFA.Seal([]byte(secret))
	st.StoreMFAEnrollment(ctx, uid, enc, "t", 0)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client) // must not reach the menu; ends at the second login quit

	if len(p.menuErrors) != 0 {
		t.Fatal("PF3 at MFA must not reach the menu")
	}
}

func TestMFADisabledWhenNoCipher(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{quit: true}},
	}
	b := &fakeBridger{}
	s := newTestSession(t, p, b) // no cipher
	ctx := context.Background()
	uid, _ := s.Store.CreateUser(ctx, "alice", "x")
	s.Store.SetMFARequired(ctx, uid, true)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)
	if len(p.menuPicks) != 0 {
		t.Fatal("with MFA cipher nil, user should pass straight to the menu")
	}
}

// fixedNow returns a deterministic clock for throttle tests.
func fixedNow() time.Time { return time.Unix(1_700_000_000, 0) }

func TestLoginThrottleBacksOffAndResets(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "bad"},  // fail 1 → 2s
			{user: "alice", pass: "bad"},  // fail 2 → 4s
			{user: "alice", pass: "good"}, // success → reset
			{quit: true},                  // logoff at next login
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.Throttle = newAuthThrottle()
	s.Now = fixedNow
	var slept []time.Duration
	s.Sleep = func(d time.Duration) { slept = append(slept, d) }

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	want := []time.Duration{2 * time.Second, 4 * time.Second}
	if len(slept) != len(want) || slept[0] != want[0] || slept[1] != want[1] {
		t.Fatalf("delays = %v, want %v", slept, want)
	}
	if n, ok := s.Throttle.peek("alice"); ok {
		t.Errorf("counter not reset on success: count=%d", n)
	}
}

func TestLoginThrottleDisabledWhenBaseZero(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins:   []loginResult{{user: "alice", pass: "bad"}, {quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	s.Throttle = newAuthThrottle()
	s.Now = fixedNow
	var slept []time.Duration
	s.Sleep = func(d time.Duration) { slept = append(slept, d) }
	if err := s.Store.SetConfig(context.Background(), "AUTH_DELAY_BASE_SECS", "0"); err != nil {
		t.Fatal(err)
	}

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	if len(slept) != 0 {
		t.Errorf("base=0 should disable throttling, slept=%v", slept)
	}
}

func TestMFAVerifyThrottleBacksOff(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_700_000_000, 0)
	step := uint64(now.Unix() / 30)
	good := codeForServer(t, secret, step)

	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		verifies: []mfaResult{
			{code: "000000"}, // wrong → 2s
			{code: "111111"}, // wrong → 4s
			{code: good},     // correct → reset
		},
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{quit: true}},
	}
	s, st := newMFATestSession(t, p, &fakeBridger{})
	s.Throttle = newAuthThrottle() // Now is already fixed by newMFATestSession
	var slept []time.Duration
	s.Sleep = func(d time.Duration) { slept = append(slept, d) }
	ctx := context.Background()
	uid, _ := st.CreateUser(ctx, "alice", "x")
	st.SetMFARequired(ctx, uid, true)
	enc, _ := s.MFA.Seal([]byte(secret))
	st.StoreMFAEnrollment(ctx, uid, enc, "2026-01-01T00:00:00Z", 0)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	want := []time.Duration{2 * time.Second, 4 * time.Second}
	if len(slept) != len(want) || slept[0] != want[0] || slept[1] != want[1] {
		t.Fatalf("mfa delays = %v, want %v", slept, want)
	}
	if n, ok := s.Throttle.peek("alice"); ok {
		t.Errorf("counter not reset after correct code: count=%d", n)
	}
}

func TestMFAEnrollThrottleBacksOff(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_700_000_000, 0)
	step := uint64(now.Unix() / 30)
	good := codeForServer(t, secret, step)

	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		enrolls: []mfaResult{
			{code: "000000"}, // wrong → 2s
			{code: "111111"}, // wrong → 4s
			{code: good},     // correct → reset
		},
		logins:    []loginResult{{user: "alice", pass: "good"}, {quit: true}},
		menuPicks: []menuResult{{quit: true}},
	}
	s, st := newMFATestSession(t, p, &fakeBridger{})
	s.MFAGenerate = func(_, _ string) (string, error) { return secret, nil }
	s.Throttle = newAuthThrottle() // Now is already fixed by newMFATestSession
	var slept []time.Duration
	s.Sleep = func(d time.Duration) { slept = append(slept, d) }
	ctx := context.Background()
	uid, _ := st.CreateUser(ctx, "alice", "x")
	st.SetMFARequired(ctx, uid, true)

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	want := []time.Duration{2 * time.Second, 4 * time.Second}
	if len(slept) != len(want) || slept[0] != want[0] || slept[1] != want[1] {
		t.Fatalf("enroll delays = %v, want %v", slept, want)
	}
	if n, ok := s.Throttle.peek("alice"); ok {
		t.Errorf("counter not reset after successful enroll: count=%d", n)
	}
}

func TestLoginThrottleEnumerationSafe(t *testing.T) {
	// A first failure for an unknown username and for a known one must produce
	// the same delay — the throttle must not reveal whether a user exists.
	delayFor := func(user string) time.Duration {
		p := &fakePresenter{
			termType: "IBM-3278-2-E",
			logins:   []loginResult{{user: user, pass: "bad"}, {quit: true}},
		}
		s := newTestSession(t, p, &fakeBridger{})
		s.Throttle = newAuthThrottle()
		s.Now = fixedNow
		var slept []time.Duration
		s.Sleep = func(d time.Duration) { slept = append(slept, d) }
		client, _ := net.Pipe()
		defer client.Close()
		s.Run(client)
		if len(slept) != 1 {
			t.Fatalf("%s: expected one delay, got %v", user, slept)
		}
		return slept[0]
	}
	if delayFor("ghost") != delayFor("alice") {
		t.Error("unknown vs known username produced different delays")
	}
}

func TestSessionAuditActorAttribution(t *testing.T) {
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "sw0rdf1sh-wrong"},
			{user: "alice", pass: "good"},
			{quit: true}, // login render after menu logoff
		},
		menuPicks: []menuResult{{quit: true}},
	}
	s := newTestSession(t, p, &fakeBridger{})
	rec := &recordingAuditor{}
	s.Auditor = rec

	client, _ := net.Pipe()
	defer client.Close()
	s.Run(client)

	want := []string{store.AuditConnect, store.AuditAuthFail, store.AuditAuthOK, store.AuditLogout, store.AuditDisconnect}
	if !slices.Equal(rec.kinds(), want) {
		t.Fatalf("kinds = %v, want %v", rec.kinds(), want)
	}
	if a := rec.events[0].Actor; a != "" { // connect: pre-auth
		t.Errorf("connect actor = %q, want empty", a)
	}
	if a := rec.events[1].Actor; a != "" { // auth_fail: never authenticated
		t.Errorf("auth_fail actor = %q, want empty", a)
	}
	// authStub returns identity.Username verbatim ("alice"), so actor == "alice".
	if a := rec.events[2].Actor; a != "alice" { // auth_ok
		t.Errorf("auth_ok actor = %q, want alice", a)
	}
	if a := rec.events[3].Actor; a != "alice" { // logout while still attributed
		t.Errorf("logout actor = %q, want alice", a)
	}
	if a := rec.events[4].Actor; a != "" { // disconnect from the login screen post-logoff
		t.Errorf("disconnect actor = %q, want empty after logoff", a)
	}
}
