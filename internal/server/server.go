package server

import (
	"errors"
	"log"
	"net"
	"sync"

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
		Store:          h.store,
		Authenticate:   auth.Authenticate,
		Presenter:      go3270Presenter{},
		Bridger:        realBridger{},
		EscapeAID:      h.escapeAID,
		AdminPresenter: go3270Presenter{},
	}
	s.Run(conn)
}

// NewSessionHandler returns a connHandler that runs a full proxy session.
func NewSessionHandler(st *store.Store, escapeAID byte) connHandler {
	return sessionHandler{store: st, escapeAID: escapeAID}
}

// ServeAll runs one accept loop per listener, all sharing handler. The first
// listener error closes the remaining listeners (unblocking their Accept) and
// is returned, so a single transport failure brings the process down cleanly
// rather than silently losing a listener.
func ServeAll(listeners []net.Listener, handler connHandler) error {
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
	for _, ln := range listeners {
		srv := &Server{Listener: ln, Handler: handler}
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
