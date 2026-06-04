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
