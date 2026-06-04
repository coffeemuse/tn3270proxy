package server

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeAddr is a stand-in net.Addr for the fake listeners below.
type fakeAddr struct{}

func (fakeAddr) Network() string { return "fake" }
func (fakeAddr) String() string  { return "fake" }

// triggerListener fails on demand: Accept blocks until fail is closed, then
// returns err (the genuine root-cause error). It models the one listener that
// fails for a real reason.
type triggerListener struct {
	fail chan struct{}
	err  error
}

func (l *triggerListener) Accept() (net.Conn, error) { <-l.fail; return nil, l.err }
func (l *triggerListener) Close() error              { return nil }
func (l *triggerListener) Addr() net.Addr            { return fakeAddr{} }

// closeListener models a healthy listener: Accept blocks until Close wakes it,
// then returns a benign "closed" error — the masking error in issue #13.
type closeListener struct {
	closed chan struct{}
	once   sync.Once
	err    error
}

func (l *closeListener) Accept() (net.Conn, error) { <-l.closed; return nil, l.err }
func (l *closeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}
func (l *closeListener) Addr() net.Addr { return fakeAddr{} }

// TestServeAllReturnsGenuineErrorNotMaskingCloseError pins issue #13: when one
// listener fails for a real reason, ServeAll must return that error, not the
// benign "use of closed network connection" that closing the healthy listeners
// produces. The benign error can only arise *after* closeAll runs, so a correct
// ServeAll records the genuine error first and never lets the close error mask
// it. This is a scheduling race in the buggy implementation, so the scenario is
// stressed in a loop (and is meant to be run under -race).
func TestServeAllReturnsGenuineErrorNotMaskingCloseError(t *testing.T) {
	genuineErr := errors.New("genuine listener failure")
	benignErr := errors.New("use of closed network connection")

	for i := range 500 {
		benign := &closeListener{closed: make(chan struct{}), err: benignErr}
		genuine := &triggerListener{fail: make(chan struct{}), err: genuineErr}

		done := make(chan error, 1)
		go func() {
			done <- ServeAll([]net.Listener{benign, genuine}, handlerFunc(func(net.Conn) {}), Limits{})
		}()

		close(genuine.fail) // trigger the real failure

		select {
		case err := <-done:
			if !errors.Is(err, genuineErr) {
				t.Fatalf("iter %d: ServeAll returned %v, want the genuine error %v (masking close error?)", i, err, genuineErr)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("iter %d: ServeAll did not return", i)
		}
	}
}

func TestServeAllDispatchesAcrossListeners(t *testing.T) {
	ln1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	handled := make(chan struct{}, 2)
	h := handlerFunc(func(c net.Conn) {
		defer c.Close()
		handled <- struct{}{}
	})

	done := make(chan error, 1)
	go func() { done <- ServeAll([]net.Listener{ln1, ln2}, h, Limits{}) }()

	for _, ln := range []net.Listener{ln1, ln2} {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatalf("dial %s: %v", ln.Addr(), err)
		}
		conn.Close()
	}

	for i := 0; i < 2; i++ {
		select {
		case <-handled:
		case <-time.After(2 * time.Second):
			t.Fatal("a handler was not invoked")
		}
	}

	ln1.Close() // first listener error should tear everything down
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeAll did not return after a listener closed")
	}
}

func TestServeAllErrorsWithNoListeners(t *testing.T) {
	if err := ServeAll(nil, handlerFunc(func(net.Conn) {}), Limits{}); err == nil {
		t.Fatal("want error for empty listener set, got nil")
	}
}

// The cap being shared across listeners is asserted structurally (one limiter
// instance wired into every Server) rather than by driving connections at the
// cap: with the pre-Accept acquire design, an idle listener's accept loop
// parks a global slot while blocked in Accept, so any at-cap multi-listener
// sequence is nondeterministic — whichever loop wins a freed slot may park it
// on a listener with no pending conns (GH issue #18). At-cap behavior itself
// is covered per-listener by TestServerGlobalCapDefersAccept.
func TestNewServersShareOneLimiter(t *testing.T) {
	ln1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln1.Close()
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()

	h := handlerFunc(func(net.Conn) {})
	servers := newServers([]net.Listener{ln1, ln2}, h, Limits{MaxConns: 8})
	if len(servers) != 2 {
		t.Fatalf("newServers returned %d servers, want 2", len(servers))
	}
	if servers[0].Limiter == nil {
		t.Fatal("Limiter is nil with MaxConns set")
	}
	if servers[0].Limiter != servers[1].Limiter {
		t.Error("listeners got distinct limiters; the cap must be process-wide, not per-listener")
	}
}

func TestNewServersZeroCapMeansNoLimiter(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	servers := newServers([]net.Listener{ln}, handlerFunc(func(net.Conn) {}), Limits{})
	if servers[0].Limiter != nil {
		t.Errorf("Limiter = %v, want nil when MaxConns is 0 (unlimited)", servers[0].Limiter)
	}
}

func TestWrapIdleInstallsWrapper(t *testing.T) {
	pipe, _ := net.Pipe()
	defer pipe.Close()

	if got := wrapIdle(pipe, 0); got != pipe {
		t.Errorf("wrapIdle with zero idle should return the conn unchanged")
	}
	wrapped := wrapIdle(pipe, time.Minute)
	if _, ok := wrapped.(*idleConn); !ok {
		t.Errorf("wrapIdle = %T, want *idleConn", wrapped)
	}
}
