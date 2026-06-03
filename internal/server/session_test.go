package server

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/bridge"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// --- fakes ---

type fakePresenter struct {
	termType     string
	rows, cols   int // 0,0 → Negotiate reports 24×80
	logins       []loginResult
	menuPicks    []menuResult
	menuErrors   []string
	loginErrors  []string
	gotAdminFlag []bool
	gotTerms     []Term // every term passed to Login/Menu, in call order
}

type loginResult struct {
	user, pass string
	quit       bool
	err        error
}
type menuResult struct {
	sel   *store.Service
	admin bool
	quit  bool
	err   error
}

func (f *fakePresenter) Negotiate(conn net.Conn) (Term, error) {
	rows, cols := f.rows, f.cols
	if rows == 0 {
		rows, cols = 24, 80
	}
	return Term{Type: f.termType, Rows: rows, Cols: cols}, nil
}

func (f *fakePresenter) Login(conn net.Conn, term Term, errMsg string) (string, string, bool, error) {
	f.gotTerms = append(f.gotTerms, term)
	f.loginErrors = append(f.loginErrors, errMsg)
	r := f.logins[0]
	f.logins = f.logins[1:]
	return r.user, r.pass, r.quit, r.err
}

func (f *fakePresenter) Menu(conn net.Conn, term Term, svcs []store.Service, admin bool, errMsg string) (*store.Service, bool, bool, error) {
	f.gotTerms = append(f.gotTerms, term)
	f.menuErrors = append(f.menuErrors, errMsg)
	f.gotAdminFlag = append(f.gotAdminFlag, admin)
	r := f.menuPicks[0]
	f.menuPicks = f.menuPicks[1:]
	return r.sel, r.admin, r.quit, r.err
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
	sid, _ := st.CreateService(ctx, "PROD", "10.0.0.1", 23, false, true)
	st.LinkGroupService(ctx, gid, sid)

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
			{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}},
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
			{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}},
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
			{sel: &store.Service{Name: "SEC", Host: "10.0.0.9", Port: 992, TLS: true, TLSVerify: true}},
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
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}},
		menuPicks: []menuResult{
			{sel: &store.Service{Name: "PROD", Host: "10.0.0.1", Port: 23}},
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
		menuPicks: []menuResult{{admin: true}, {quit: true}},
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

func TestSessionNonAdminAdminSelIgnored(t *testing.T) {
	// Even if a (buggy) presenter reports adminSel=true for a non-admin,
	// the session's isAdmin double-guard must not run the admin flow.
	p := &fakePresenter{
		termType: "IBM-3278-2-E",
		logins: []loginResult{
			{user: "alice", pass: "good"}, // ops, not ZZADMIN
			{quit: true},                  // second login render after menu logoff
		},
		menuPicks: []menuResult{{admin: true}, {quit: true}},
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
