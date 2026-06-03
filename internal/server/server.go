package server

import (
	"net"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// connHandler handles a single accepted connection.
type connHandler interface {
	Handle(conn net.Conn)
}

// Server accepts TCP connections and dispatches each to Handler in its own
// goroutine.
type Server struct {
	Listener net.Listener
	Handler  connHandler
}

// Serve runs the accept loop until the listener is closed.
func (s *Server) Serve() error {
	for {
		conn, err := s.Listener.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer func() {
		_ = recover()
		conn.Close()
	}()
	s.Handler.Handle(conn)
}

// sessionHandler builds a fresh Session per connection.
type sessionHandler struct {
	store     *store.Store
	escapeAID byte
}

func (h sessionHandler) Handle(conn net.Conn) {
	s := &Session{
		Store:        h.store,
		Authenticate: auth.Authenticate,
		Presenter:    go3270Presenter{},
		Bridger:      realBridger{},
		EscapeAID:    h.escapeAID,
	}
	s.Run(conn)
}

// NewSessionHandler returns a connHandler that runs a full proxy session.
func NewSessionHandler(st *store.Store, escapeAID byte) connHandler {
	return sessionHandler{store: st, escapeAID: escapeAID}
}
