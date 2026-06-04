package server

import (
	"net"
	"testing"
	"time"
)

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

func TestServeAllSharesGlobalCapAcrossListeners(t *testing.T) {
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

	h := &blockingHandler{started: make(chan net.Conn, 8), release: make(chan struct{}, 8)}
	go ServeAll([]net.Listener{ln1, ln2}, h, Limits{MaxConns: 1})

	// One conn on listener 1 consumes the single shared slot…
	dialT(t, ln1.Addr().String())
	waitStarted(t, h)

	// …so a conn on listener 2 must wait, proving the cap is shared, not per-listener.
	dialT(t, ln2.Addr().String())
	assertNotStarted(t, h)

	h.release <- struct{}{}
	waitStarted(t, h)
	h.release <- struct{}{}
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
