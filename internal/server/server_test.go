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
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
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

func TestSessionHandlerSetsTrustAndRegimeFields(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	limits := Limits{
		PreAuthIdle:      2 * time.Minute,
		Idle:             30 * time.Minute,
		PreAuthMax:       5 * time.Minute,
		BridgeIdleExempt: true,
		Trust:            StaticTrustChecker(netip.MustParsePrefix("10.0.0.0/24")),
	}
	h := NewSessionHandler(st, 0x6B, limits, slog.Default(), "v9.9.9", nil).(sessionHandler)
	connLog := slog.Default()

	got := h.sessionFor(&net.TCPAddr{IP: net.ParseIP("10.0.0.9"), Port: 1}, connLog)
	if !got.Trusted {
		t.Error("client in trusted CIDR should yield Trusted session")
	}
	if got.PreAuthMax != 5*time.Minute || !got.BridgeIdleExempt {
		t.Errorf("regime fields not propagated: %+v", got)
	}
	if got.Release != "v9.9.9" {
		t.Errorf("Session.Release = %q, want v9.9.9", got.Release)
	}
	if got.RemoteHost != "10.0.0.9" {
		t.Errorf("RemoteHost = %q, want 10.0.0.9", got.RemoteHost)
	}
	untrusted := h.sessionFor(&net.TCPAddr{IP: net.ParseIP("10.9.9.9"), Port: 1}, connLog)
	if untrusted.Trusted {
		t.Error("client outside trusted CIDRs must not be Trusted")
	}
}

func TestHostOnly(t *testing.T) {
	cases := []struct {
		name string
		addr net.Addr
		want string
	}{
		{"ipv4 host:port", &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 51324}, "203.0.113.7"},
		{"ipv6 host:port", &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 23}, "2001:db8::1"},
		{"nil addr", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hostOnly(tc.addr); got != tc.want {
				t.Errorf("hostOnly(%v) = %q, want %q", tc.addr, got, tc.want)
			}
		})
	}
}

func TestHandlerSharesThrottleAcrossSessions(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	h := NewSessionHandler(st, 0x6B, Limits{}, nil, "", nil).(sessionHandler)
	a1 := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
	a2 := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 2}
	s1 := h.sessionFor(a1, slog.Default())
	s2 := h.sessionFor(a2, slog.Default())

	if s1.Throttle == nil {
		t.Fatal("session throttle is nil")
	}
	if s1.Throttle != s2.Throttle {
		t.Error("sessions from one handler must share the throttle")
	}
}
