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
	go func() { done <- ServeAll([]net.Listener{ln1, ln2}, h) }()

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
	if err := ServeAll(nil, handlerFunc(func(net.Conn) {})); err == nil {
		t.Fatal("want error for empty listener set, got nil")
	}
}
