package server

import (
	"net"
	"sync"
	"testing"
	"time"
)

type handlerFunc func(net.Conn)

func (h handlerFunc) Handle(c net.Conn) { h(c) }

func TestServerAcceptsAndDispatches(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	handled := make(chan struct{}, 1)
	srv := &Server{
		Listener: ln,
		Handler: handlerFunc(func(c net.Conn) {
			defer c.Close()
			handled <- struct{}{}
		}),
	}
	go func() {
		defer wg.Done()
		srv.Serve()
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	select {
	case <-handled:
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not invoked")
	}

	ln.Close()
	wg.Wait()
}
