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
	termType   string
	logins     []loginResult
	menuPicks  []menuResult
	menuErrors []string
}

type loginResult struct {
	user, pass string
	quit       bool
	err        error
}
type menuResult struct {
	sel  *store.Service
	quit bool
	err  error
}

func (f *fakePresenter) Negotiate(conn net.Conn) (string, error) { return f.termType, nil }

func (f *fakePresenter) Login(conn net.Conn) (string, string, bool, error) {
	r := f.logins[0]
	f.logins = f.logins[1:]
	return r.user, r.pass, r.quit, r.err
}

func (f *fakePresenter) Menu(conn net.Conn, svcs []store.Service, errMsg string) (*store.Service, bool, error) {
	f.menuErrors = append(f.menuErrors, errMsg)
	r := f.menuPicks[0]
	f.menuPicks = f.menuPicks[1:]
	return r.sel, r.quit, r.err
}

type fakeBridger struct {
	causes []bridge.Cause
	errs   []error
	calls  int
}

func (f *fakeBridger) Bridge(conn net.Conn, addr, termType string, escapeAID byte) (bridge.Cause, error) {
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
	sid, _ := st.CreateService(ctx, "PROD", "10.0.0.1", 23, false)
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
}

func TestSessionEscapeReturnsToMenu(t *testing.T) {
	p := &fakePresenter{
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}},
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
		termType:  "IBM-3278-2-E",
		logins:    []loginResult{{user: "alice", pass: "good"}},
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

	if len(p.menuErrors) < 2 || p.menuErrors[1] == "" {
		t.Errorf("expected error message on menu after backend failure; got %v", p.menuErrors)
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
