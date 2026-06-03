// Package server runs the proxy: accepting connections and driving each one
// through the login → menu → bridge state machine.
package server

import (
	"context"
	"errors"
	"net"
	"strconv"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/bridge"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// Presenter renders the proxy's own 3270 screens to the client. The real
// implementation (Task 14) wraps go3270; tests use a fake.
type Presenter interface {
	Negotiate(conn net.Conn) (termType string, err error)
	Login(conn net.Conn) (username, password string, quit bool, err error)
	Menu(conn net.Conn, services []store.Service, errMsg string) (selected *store.Service, quit bool, err error)
}

// Bridger connects the client to a backend service.
type Bridger interface {
	Bridge(client net.Conn, addr, termType string, escapeAID byte) (bridge.Cause, error)
}

// Authenticator verifies credentials against a store. Matches auth.Authenticate.
type Authenticator func(ctx context.Context, st auth.UserStore, username, password string) (auth.Identity, error)

// Session drives one client connection through its lifecycle.
type Session struct {
	Store        *store.Store
	Authenticate Authenticator
	Presenter    Presenter
	Bridger      Bridger
	EscapeAID    byte
}

// Run executes the session state machine for one connection. It returns when
// the user disconnects or an unrecoverable error occurs. It does not close conn
// (the caller owns it).
func (s *Session) Run(conn net.Conn) {
	ctx := context.Background()

	termType, err := s.Presenter.Negotiate(conn)
	if err != nil {
		return
	}

	identity, ok := s.doLogin(ctx, conn)
	if !ok {
		return
	}

	errMsg := ""
	for {
		services, err := s.Store.ListServicesForGroups(ctx, identity.Groups)
		if err != nil {
			return
		}
		selected, quit, err := s.Presenter.Menu(conn, services, errMsg)
		if err != nil || quit {
			return
		}
		errMsg = ""
		if selected == nil {
			continue
		}

		addr := net.JoinHostPort(selected.Host, strconv.Itoa(selected.Port))
		cause, berr := s.Bridger.Bridge(conn, addr, termType, s.EscapeAID)
		switch cause {
		case bridge.CauseClientClosed:
			return
		case bridge.CauseError:
			errMsg = "Could not connect to " + selected.Name
			if berr == nil {
				errMsg = "Session error on " + selected.Name
			}
		default:
			// CauseBackendClosed or CauseUserEscaped → back to the menu.
		}
	}
}

// doLogin loops the login screen until success, or returns ok=false if the
// user quits.
func (s *Session) doLogin(ctx context.Context, conn net.Conn) (auth.Identity, bool) {
	for {
		user, pass, quit, err := s.Presenter.Login(conn)
		if err != nil || quit {
			return auth.Identity{}, false
		}
		identity, err := s.Authenticate(ctx, s.Store, user, pass)
		if err == nil {
			return identity, true
		}
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			return auth.Identity{}, false
		}
	}
}
