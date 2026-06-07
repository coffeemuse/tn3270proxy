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

package dummy

import (
	"math/rand"
	"net"

	"github.com/racingmars/go3270"
)

// allExitAIDs lists every attention key so HandleScreen returns to us on ANY
// keypress; we then repaint a freshly-random screen. There are no input fields,
// so nothing is a validated "submit" and pfkeys is nil.
var allExitAIDs = []go3270.AID{
	go3270.AIDEnter, go3270.AIDClear,
	go3270.AIDPA1, go3270.AIDPA2, go3270.AIDPA3,
	go3270.AIDPF1, go3270.AIDPF2, go3270.AIDPF3, go3270.AIDPF4,
	go3270.AIDPF5, go3270.AIDPF6, go3270.AIDPF7, go3270.AIDPF8,
	go3270.AIDPF9, go3270.AIDPF10, go3270.AIDPF11, go3270.AIDPF12,
	go3270.AIDPF13, go3270.AIDPF14, go3270.AIDPF15, go3270.AIDPF16,
	go3270.AIDPF17, go3270.AIDPF18, go3270.AIDPF19, go3270.AIDPF20,
	go3270.AIDPF21, go3270.AIDPF22, go3270.AIDPF23, go3270.AIDPF24,
}

// Server is a minimal TN3270 server that paints a random mockup welcome screen
// on each connection. It keeps no session state.
type Server struct {
	ln net.Listener
}

// Listen binds a plaintext TCP listener on addr (e.g. ":3300").
func Listen(addr string) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Server{ln: ln}, nil
}

// Addr reports the bound address (useful when addr requested port 0).
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// Close stops the listener.
func (s *Server) Close() error { return s.ln.Close() }

// Serve accepts connections until the listener is closed, handling each in its
// own goroutine. It returns the Accept error that stopped it.
func (s *Server) Serve() error {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return err
		}
		go serveConn(conn)
	}
}

// ListenAndServe binds addr and serves until the listener errors.
func ListenAndServe(addr string) error {
	s, err := Listen(addr)
	if err != nil {
		return err
	}
	return s.Serve()
}

// serveConn negotiates Telnet then repaints a random welcome screen on every
// keypress until the client disconnects. A recover() keeps one bad client from
// taking down the listener. No logging, by design.
func serveConn(conn net.Conn) {
	defer func() { _ = recover() }() // outermost: catch panics from anything below
	defer conn.Close()

	if _, err := go3270.NegotiateTelnet(conn); err != nil {
		return
	}
	for {
		if _, err := go3270.HandleScreen(
			randomScreen(), nil, map[string]string{},
			nil, allExitAIDs, "", 0, 0, conn,
		); err != nil {
			return
		}
	}
}

// randomScreen returns one of the welcome screens at random.
func randomScreen() go3270.Screen { return screenFor(rand.Intn(len(builders))) }
