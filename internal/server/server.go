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
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/mfa"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// Limits carries the connection-hardening knobs (GH issue #1) into the server
// layer; it mirrors config.Limits without importing the config package. Zero
// values disable the corresponding control.
type Limits struct {
	PreAuthIdle      time.Duration // idle deadline before login
	Idle             time.Duration // idle deadline after login (incl. bridges unless BridgeIdleExempt)
	MaxConns         int           // global concurrent-connection cap
	MaxPerIP         int           // per-client-IP cap
	PreAuthMax       time.Duration // absolute deadline to authenticate (GH #18)
	BridgeIdleExempt bool          // no idle timeout during an active bridge
	// Trust decides whether a connecting client is exempt from the pre-auth
	// timers and the per-IP connection cap. nil trusts nobody.
	Trust TrustChecker
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
	// Trust exempts matching client IPs from the pre-auth timers and the
	// per-IP cap (GH #18). nil trusts nobody.
	Trust TrustChecker
	// Logger is used for accept/reject log lines; nil falls back to slog.Default().
	Logger *slog.Logger
}

func (s *Server) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
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
		trusted := s.Trust != nil && s.Trust.IsTrusted(conn.RemoteAddr())
		if !s.Limiter.admitIP(conn.RemoteAddr(), trusted) {
			s.log().Warn("per-IP connection cap reached; closing connection", "remote", conn.RemoteAddr())
			conn.Close()
			s.Limiter.releaseGlobal()
			continue
		}
		s.log().Info("accepted connection", "remote", conn.RemoteAddr(), "trusted", trusted)
		go s.handle(conn, trusted)
	}
}

func (s *Server) handle(conn net.Conn, trusted bool) {
	defer func() {
		if r := recover(); r != nil {
			s.log().Error("session panic", "remote", conn.RemoteAddr(), "panic", r)
		}
		conn.Close()
		s.Limiter.releaseIP(conn.RemoteAddr(), trusted)
		s.Limiter.releaseGlobal()
	}()
	s.Handler.Handle(conn)
}

// sessionHandler builds a fresh Session per connection.
type sessionHandler struct {
	store     *store.Store
	escapeAID byte
	limits    Limits
	logger    *slog.Logger
	release   string
	mfaCipher *mfa.Cipher
}

// wrapIdle installs the idle-deadline wrapper when an idle window is set.
func wrapIdle(conn net.Conn, preAuthIdle time.Duration) net.Conn {
	if preAuthIdle <= 0 {
		return conn
	}
	return newIdleConn(conn, preAuthIdle)
}

// sessionFor builds the Session for a connection from addr, deciding trust and
// carrying the regime knobs from limits. connLog is the per-connection logger
// (already enriched with "remote").
func (h sessionHandler) sessionFor(addr net.Addr, connLog *slog.Logger) *Session {
	trusted := h.limits.Trust != nil && h.limits.Trust.IsTrusted(addr)
	return &Session{
		Store:            h.store,
		Authenticate:     auth.Authenticate,
		Presenter:        go3270Presenter{},
		Bridger:          realBridger{},
		EscapeAID:        h.escapeAID,
		AdminPresenter:   go3270Presenter{},
		Auditor:          storeAuditor{store: h.store, logger: connLog},
		Logger:           connLog,
		Release:          h.release,
		PreAuthIdle:      h.limits.PreAuthIdle,
		Idle:             h.limits.Idle,
		PreAuthMax:       h.limits.PreAuthMax,
		Trusted:          trusted,
		BridgeIdleExempt: h.limits.BridgeIdleExempt,
		MFA:              h.mfaCipher,
	}
}

func (h sessionHandler) Handle(conn net.Conn) {
	connLog := h.logger.With("remote", conn.RemoteAddr().String())
	s := h.sessionFor(conn.RemoteAddr(), connLog)
	s.Run(wrapIdle(conn, h.limits.PreAuthIdle))
}

// NewSessionHandler returns a connHandler that runs a full proxy session.
// Connections are wrapped with the idle-deadline enforcer per limits.
// logger is the base logger; each accepted connection receives a child logger
// tagged with "remote" (and later "user" after authentication).
// release is the resolved build version (e.g. "v1.2.3" or a VCS hash) shown in
// the menu status block (GH #53). mfaCipher encrypts/decrypts TOTP secrets;
// nil disables MFA (GH #47).
func NewSessionHandler(st *store.Store, escapeAID byte, limits Limits, logger *slog.Logger, release string, mfaCipher *mfa.Cipher) connHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return sessionHandler{store: st, escapeAID: escapeAID, limits: limits, logger: logger, release: release, mfaCipher: mfaCipher}
}

// newServers builds one Server per listener, all sharing handler and one
// connection limiter (the caps in limits are process-wide, not per-listener;
// MaxConns 0 means unlimited).
func newServers(listeners []net.Listener, handler connHandler, limits Limits) []*Server {
	// Extract the logger from the handler when it is a sessionHandler, so that
	// accept-level log lines share the same logger as the session pipeline.
	var logger *slog.Logger
	if sh, ok := handler.(sessionHandler); ok {
		logger = sh.logger
	}
	var limiter *connLimiter
	if limits.MaxConns > 0 {
		limiter = newConnLimiter(limits.MaxConns, limits.MaxPerIP, logger)
	}
	servers := make([]*Server, len(listeners))
	for i, ln := range listeners {
		servers[i] = &Server{Listener: ln, Handler: handler, Limiter: limiter, Trust: limits.Trust, Logger: logger}
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
	closeAll := func() {
		for _, ln := range listeners {
			ln.Close()
		}
	}
	// The first listener to fail records its error and triggers closeAll, both
	// guarded by the same sync.Once. closeAll wakes the healthy listeners'
	// Accept with a benign "use of closed network connection", but those errors
	// reach record only after the Once has fired, so they are dropped and can
	// never mask the genuine root cause (issue #13).
	var (
		once  sync.Once
		first error
		wg    sync.WaitGroup
	)
	record := func(err error) {
		once.Do(func() {
			first = err
			closeAll()
		})
	}
	for _, srv := range newServers(listeners, handler, limits) {
		wg.Go(func() {
			record(srv.Serve())
		})
	}
	wg.Wait()
	return first
}
