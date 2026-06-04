// Package server runs the proxy: accepting connections and driving each one
// through the login → menu → bridge state machine.
package server

import (
	"context"
	"errors"
	"log"
	"net"
	"slices"
	"strconv"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/bridge"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// Presenter renders the proxy's own 3270 screens to the client. The real
// implementation wraps go3270; tests use a fake. The Term returned by
// Negotiate must be passed back into every subsequent call so screens render
// at the client's negotiated size and codepage.
type Presenter interface {
	Negotiate(conn net.Conn) (Term, error)
	Login(conn net.Conn, term Term, errMsg string) (username, password string, quit bool, err error)
	Menu(conn net.Conn, term Term, services []store.Service, admin bool, errMsg string) (selected *store.Service, adminSel bool, quit bool, err error)
}

// BackendTLS expresses a service's backend-TLS intent. The server layer keeps
// crypto/tls out of the session machine; realBridger turns this into a
// *tls.Config.
type BackendTLS struct {
	Enabled bool // dial the backend over TLS
	Verify  bool // validate the backend cert (system roots + hostname)
}

// Bridger connects the client to a backend service.
type Bridger interface {
	Bridge(client net.Conn, addr, termType string, escapeAID byte, btls BackendTLS) (bridge.Cause, error)
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
	// AdminPresenter renders the admin screens. When nil (or the user is not
	// in store.AdminGroup) the menu shows no admin entry.
	AdminPresenter AdminPresenter
	// Auditor records the session's audit trail; nil disables auditing.
	Auditor Auditor
}

// Run executes the session state machine for one connection. It returns when
// the user disconnects or an unrecoverable error occurs. It does not close conn
// (the caller owns it).
func (s *Session) Run(conn net.Conn) {
	ctx := context.Background()
	aud := s.newAuditTrail(conn)

	aud.record(ctx, store.AuditEvent{Kind: store.AuditConnect})
	endDetail := "client disconnected"
	currentUser := ""
	defer func() {
		aud.record(ctx, store.AuditEvent{
			Kind: store.AuditDisconnect, Username: currentUser, Detail: endDetail})
	}()

	term, err := s.Presenter.Negotiate(conn)
	if err != nil {
		log.Printf("telnet negotiation failed: %v", err)
		endDetail = "negotiation failed"
		return
	}

	// Each outer iteration is one login → menu lifetime: PF3 at the menu logs
	// off (back to the login screen); PF3 at the login screen disconnects.
	// Re-login re-evaluates groups, so a demoted admin loses the A entry at
	// logoff.
	for {
		identity, ok := s.doLogin(ctx, conn, term, aud)
		if !ok {
			endDetail = "quit at login"
			return
		}
		currentUser = identity.Username

		isAdmin := s.AdminPresenter != nil && slices.Contains(identity.Groups, store.AdminGroup)
		errMsg := ""
	menu:
		for {
			services, err := s.Store.ListServicesForGroups(ctx, identity.Groups)
			if err != nil {
				log.Printf("listing services for user %s failed: %v", identity.Username, err)
				services = nil
				errMsg = "Temporary error retrieving services; try again"
			}
			selected, adminSel, quit, err := s.Presenter.Menu(conn, term, services, isAdmin, errMsg)
			if err != nil {
				endDetail = "menu render error"
				return
			}
			if quit {
				currentUser = ""
				break menu // logoff: back to the login screen
			}
			errMsg = ""
			if adminSel && isAdmin {
				flow := &adminFlow{store: s.Store, presenter: s.AdminPresenter, identity: identity, term: term}
				if aerr := flow.Run(ctx, conn); aerr != nil {
					log.Printf("admin flow for %s ended: %v", identity.Username, aerr)
					endDetail = "admin flow error"
					return
				}
				continue // re-render the menu: fresh service list shows admin edits
			}
			if selected == nil {
				continue
			}

			addr := net.JoinHostPort(selected.Host, strconv.Itoa(selected.Port))
			btls := BackendTLS{Enabled: selected.TLS, Verify: selected.TLSVerify}
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditBridgeStart, Username: identity.Username, Service: selected.Name})
			cause, berr := s.Bridger.Bridge(conn, addr, term.Type, s.EscapeAID, btls)
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditBridgeEnd, Username: identity.Username,
				Service: selected.Name, Detail: causeDetail(cause, berr)})
			switch cause {
			case bridge.CauseClientClosed:
				endDetail = "client closed during bridge"
				return
			case bridge.CauseError:
				log.Printf("bridge error to %s (%s): %v", selected.Name, addr, berr)
				errMsg = "Could not connect to " + selected.Name
				if berr == nil {
					errMsg = "Session error on " + selected.Name
				}
			default:
				// CauseBackendClosed or CauseUserEscaped → back to the menu.
			}
		}
	}
}

// doLogin loops the login screen until success, or returns ok=false if the
// user quits.
func (s *Session) doLogin(ctx context.Context, conn net.Conn, term Term, aud *auditTrail) (auth.Identity, bool) {
	errMsg := ""
	for {
		user, pass, quit, err := s.Presenter.Login(conn, term, errMsg)
		if err != nil || quit {
			return auth.Identity{}, false
		}
		identity, err := s.Authenticate(ctx, s.Store, user, pass)
		if err == nil {
			aud.record(ctx, store.AuditEvent{Kind: store.AuditAuthOK, Username: identity.Username})
			return identity, true
		}
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			return auth.Identity{}, false
		}
		// Attempted username only — never the password (CLAUDE.md hard rule).
		aud.record(ctx, store.AuditEvent{Kind: store.AuditAuthFail, Username: user})
		// Generic message — never reveals whether the username exists (spec §7).
		errMsg = "Invalid userid or password"
	}
}

// causeDetail renders a bridge outcome for the audit trail.
func causeDetail(c bridge.Cause, err error) string {
	switch c {
	case bridge.CauseBackendClosed:
		return "backend_closed"
	case bridge.CauseClientClosed:
		return "client_closed"
	case bridge.CauseUserEscaped:
		return "user_escaped"
	case bridge.CauseError:
		if err != nil {
			return "error: " + err.Error()
		}
		return "error"
	}
	return "unknown"
}
