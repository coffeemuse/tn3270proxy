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
	"errors"
	"log"
	"net"
	"sync"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// Limits carries the connection-hardening knobs (GH issue #1) into the server
// layer; it mirrors config.Limits without importing the config package. Zero
// values disable the corresponding control.
type Limits struct {
	PreAuthIdle time.Duration // idle deadline before login
	Idle        time.Duration // idle deadline after login (incl. bridged sessions)
	MaxConns    int           // global concurrent-connection cap
	MaxPerIP    int           // per-client-IP cap
}

// connHandler handles a single accepted connection.
type connHandler interface {
	Handle(conn net.Conn)
}

// Server accepts TCP connections and dispatches each to Handler in its own
// goroutine.
type Server struct {
	Listener net.Listener
	Handler  connHandler
	// Limiter bounds concurrent connections; nil means unlimited.
	Limiter *connLimiter
}

// Serve runs the accept loop until the listener is closed. The global
// connection slot is claimed before Accept (over-cap conns queue in the
// kernel backlog at no cost to the process); the per-IP cap needs the remote
// address, so it is enforced after Accept by closing the conn.
func (s *Server) Serve() error {
	for {
		s.Limiter.acquire()
		conn, err := s.Listener.Accept()
		if err != nil {
			s.Limiter.releaseGlobal()
			return err
		}
		if !s.Limiter.admitIP(conn.RemoteAddr()) {
			log.Printf("per-ip connection cap reached; rejecting %s", conn.RemoteAddr())
			conn.Close()
			s.Limiter.releaseGlobal()
			continue
		}
		log.Printf("accepted connection from %s", conn.RemoteAddr())
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("session panic from %s: %v", conn.RemoteAddr(), r)
		}
		conn.Close()
		s.Limiter.releaseIP(conn.RemoteAddr())
		s.Limiter.releaseGlobal()
	}()
	s.Handler.Handle(conn)
}

// sessionHandler builds a fresh Session per connection.
type sessionHandler struct {
	store     *store.Store
	escapeAID byte
	limits    Limits
}

// wrapIdle installs the idle-deadline wrapper when an idle window is set.
func wrapIdle(conn net.Conn, preAuthIdle time.Duration) net.Conn {
	if preAuthIdle <= 0 {
		return conn
	}
	return newIdleConn(conn, preAuthIdle)
}

func (h sessionHandler) Handle(conn net.Conn) {
	s := &Session{
		Store:          h.store,
		Authenticate:   auth.Authenticate,
		Presenter:      go3270Presenter{},
		Bridger:        realBridger{},
		EscapeAID:      h.escapeAID,
		AdminPresenter: go3270Presenter{},
		Auditor:        storeAuditor{store: h.store},
		PreAuthIdle:    h.limits.PreAuthIdle,
		Idle:           h.limits.Idle,
	}
	s.Run(wrapIdle(conn, h.limits.PreAuthIdle))
}

// NewSessionHandler returns a connHandler that runs a full proxy session.
// Connections are wrapped with the idle-deadline enforcer per limits.
func NewSessionHandler(st *store.Store, escapeAID byte, limits Limits) connHandler {
	return sessionHandler{store: st, escapeAID: escapeAID, limits: limits}
}

// newServers builds one Server per listener, all sharing handler and one
// connection limiter (the caps in limits are process-wide, not per-listener;
// MaxConns 0 means unlimited).
func newServers(listeners []net.Listener, handler connHandler, limits Limits) []*Server {
	var limiter *connLimiter
	if limits.MaxConns > 0 {
		limiter = newConnLimiter(limits.MaxConns, limits.MaxPerIP)
	}
	servers := make([]*Server, len(listeners))
	for i, ln := range listeners {
		servers[i] = &Server{Listener: ln, Handler: handler, Limiter: limiter}
	}
	return servers
}

// ServeAll runs one accept loop per listener, all sharing handler and one
// connection limiter (the caps in limits are process-wide, not per-listener).
// The first listener error closes the remaining listeners (unblocking their
// Accept) and is returned, so a single transport failure brings the process
// down cleanly rather than silently losing a listener.
func ServeAll(listeners []net.Listener, handler connHandler, limits Limits) error {
	if len(listeners) == 0 {
		return errors.New("server: no listeners")
	}
	errc := make(chan error, len(listeners))
	var once sync.Once
	closeAll := func() {
		for _, ln := range listeners {
			ln.Close()
		}
	}
	for _, srv := range newServers(listeners, handler, limits) {
		go func() {
			err := srv.Serve()
			once.Do(closeAll)
			errc <- err
		}()
	}
	first := <-errc
	for i := 1; i < len(listeners); i++ {
		<-errc
	}
	return first
}
