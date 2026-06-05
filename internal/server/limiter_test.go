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
	"io"
	"net"
	"testing"
	"time"
)

// blockingHandler signals each handled conn and holds it until released.
type blockingHandler struct {
	started chan net.Conn
	release chan struct{}
}

func (h *blockingHandler) Handle(conn net.Conn) {
	h.started <- conn
	<-h.release
}

// startLimitedServer runs a Server with the given limiter on a loopback
// listener and returns its address and handler.
func startLimitedServer(t *testing.T, lim *connLimiter) (string, *blockingHandler) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	h := &blockingHandler{started: make(chan net.Conn, 8), release: make(chan struct{}, 8)}
	srv := &Server{Listener: ln, Handler: h, Limiter: lim}
	go srv.Serve()
	return ln.Addr().String(), h
}

func dialT(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func waitStarted(t *testing.T, h *blockingHandler) net.Conn {
	t.Helper()
	select {
	case c := <-h.started:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not invoked within 2s")
		return nil
	}
}

func assertNotStarted(t *testing.T, h *blockingHandler) {
	t.Helper()
	select {
	case <-h.started:
		t.Fatal("handler invoked while at the connection cap")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestServerGlobalCapDefersAccept(t *testing.T) {
	addr, h := startLimitedServer(t, newConnLimiter(1, 0, nil))

	dialT(t, addr)
	waitStarted(t, h)

	dialT(t, addr) // sits in the kernel backlog: cap is full
	assertNotStarted(t, h)

	h.release <- struct{}{} // first handler finishes → slot frees
	waitStarted(t, h)       // queued conn is now served
}

func TestServerPerIPCapRejects(t *testing.T) {
	addr, h := startLimitedServer(t, newConnLimiter(8, 1, nil))

	dialT(t, addr)
	waitStarted(t, h)

	// Second conn from the same IP (loopback) must be closed by the server.
	c2 := dialT(t, addr)
	c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c2.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("second conn Read = %v, want EOF (server-side close)", err)
	}
	assertNotStarted(t, h)

	// Releasing the first frees the per-IP slot: a third conn is admitted.
	h.release <- struct{}{}
	dialT(t, addr)
	waitStarted(t, h)
}

func TestServerNilLimiterUnlimited(t *testing.T) {
	addr, h := startLimitedServer(t, nil)
	dialT(t, addr)
	dialT(t, addr)
	waitStarted(t, h)
	waitStarted(t, h)
	h.release <- struct{}{}
	h.release <- struct{}{}
}

func TestConnLimiterPerIPCounting(t *testing.T) {
	l := newConnLimiter(8, 2, nil)
	a := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1}
	b := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 1}

	for i := range 2 {
		if !l.admitIP(a, false) {
			t.Fatalf("conn %d from A should be admitted", i+1)
		}
	}
	if l.admitIP(a, false) {
		t.Fatal("third conn from A should be rejected at max_per_ip=2")
	}
	if !l.admitIP(b, false) {
		t.Fatal("conn from B should be admitted (independent count)")
	}
	l.releaseIP(a, false)
	if !l.admitIP(a, false) {
		t.Fatal("conn from A should be admitted again after a release")
	}
}

func TestAdmitIPTrustedBypassesCap(t *testing.T) {
	l := newConnLimiter(10, 1, nil) // per-IP cap of 1
	addr := &net.TCPAddr{IP: net.ParseIP("10.0.0.5"), Port: 5000}

	if !l.admitIP(addr, false) {
		t.Fatal("first untrusted admit should succeed")
	}
	if l.admitIP(addr, false) {
		t.Fatal("second untrusted admit should hit the per-IP cap")
	}
	// Trusted bypasses the cap and does not consume a per-IP slot.
	if !l.admitIP(addr, true) {
		t.Fatal("trusted admit should bypass the per-IP cap")
	}
	l.releaseIP(addr, true) // must be a no-op (never admitted a slot)
	// The original untrusted slot is still held → still at cap.
	if l.admitIP(addr, false) {
		t.Fatal("untrusted slot should still be held after trusted release")
	}
}
