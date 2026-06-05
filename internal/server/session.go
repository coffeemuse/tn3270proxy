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
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/bridge"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/CoffeeMuse/tn3270proxy/internal/ui3270"
)

// Presenter renders the proxy's own 3270 screens to the client. The real
// implementation wraps go3270; tests use a fake. The Term returned by
// Negotiate must be passed back into every subsequent call so screens render
// at the client's negotiated size and codepage.
type Presenter interface {
	Negotiate(conn net.Conn) (Term, error)
	Login(conn net.Conn, term Term, errMsg string) (username, password string, quit bool, err error)
	Menu(conn net.Conn, term Term, services []store.Service, admin bool, errMsg string) (selected *store.Service, adminSel bool, quit bool, err error)
	// News shows the MOTD pages (already paginated) one at a time: ENTER
	// advances, the last ENTER returns nil. PA3/PF3 are silent no-ops. A
	// non-nil error is a disconnect or an idle timeout (classified by the
	// caller). News is only called with at least one page.
	News(conn net.Conn, term Term, pages [][]string) error
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
	// AdminRenderer builds the ui3270.Renderer that drives the admin list/form
	// flow. Nil selects the production go3270 renderer; tests inject a fake.
	AdminRenderer func(conn net.Conn, term Term) ui3270.Renderer
	// Auditor records the session's audit trail; nil disables auditing.
	Auditor Auditor
	// PreAuthIdle/Idle are the idle windows applied to conns that implement
	// idleRegime (the idleConn wrapper installed by the accept path): Idle
	// after a successful login, PreAuthIdle again at logoff. Zero values
	// leave the connection untouched.
	PreAuthIdle time.Duration
	Idle        time.Duration
	// PreAuthMax bounds total time to authenticate (absolute, re-armed at every
	// return to the login screen). Trusted skips all pre-auth timers; the
	// per-connection trust decision is made by the handler. BridgeIdleExempt
	// disables the idle deadline during an active bridge. (GH #18)
	PreAuthMax       time.Duration
	Trusted          bool
	BridgeIdleExempt bool
}

// idleRegime is implemented by *idleConn (and test fakes); the session switches
// the connection's idle regime at each lifecycle transition. A connection that
// doesn't implement it (idle hardening disabled) is left untouched.
type idleRegime interface {
	setPreAuth(idle, max time.Duration)
	setWindow(idle time.Duration)
}

// armPreAuth enters the pre-auth regime: trusted connections are exempt; others
// get the idle window plus a fresh absolute ceiling. No-op when idle hardening
// is off (PreAuthIdle<=0) and the client isn't trusted.
func (s *Session) armPreAuth(conn net.Conn) {
	r, ok := conn.(idleRegime)
	if !ok {
		return
	}
	if s.Trusted {
		r.setWindow(0) // exempt: park at login indefinitely
		return
	}
	if s.PreAuthIdle <= 0 {
		return
	}
	if s.PreAuthMax > 0 {
		r.setPreAuth(s.PreAuthIdle, s.PreAuthMax)
	} else {
		r.setWindow(s.PreAuthIdle) // no absolute ceiling configured: idle window only
	}
}

// armPostAuth enters the post-auth regime (menu/admin): a plain idle window.
func (s *Session) armPostAuth(conn net.Conn) {
	if s.Idle <= 0 {
		return
	}
	if r, ok := conn.(idleRegime); ok {
		r.setWindow(s.Idle)
	}
}

// armBridge enters the bridge regime: the post-auth idle window, or no deadline
// when bridge idle is exempt.
func (s *Session) armBridge(conn net.Conn) {
	r, ok := conn.(idleRegime)
	if !ok {
		return
	}
	if s.BridgeIdleExempt {
		r.setWindow(0)
		return
	}
	if s.Idle <= 0 {
		return
	}
	r.setWindow(s.Idle)
}

// isTimeoutErr reports whether err is a net timeout (an idle deadline firing).
func isTimeoutErr(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// Run executes the session state machine for one connection. It returns when
// the user disconnects or an unrecoverable error occurs. It does not close conn
// (the caller owns it).
func (s *Session) Run(conn net.Conn) {
	ctx := context.Background()
	aud := s.newAuditTrail(conn)
	s.armPreAuth(conn)

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
		if isTimeoutErr(err) {
			endDetail = "idle timeout"
		}
		return
	}

	// Each outer iteration is one login → menu lifetime: PF3 at the menu logs
	// off (back to the login screen); PF3 at the login screen disconnects.
	// Re-login re-evaluates groups, so a demoted admin loses the A entry at
	// logoff.
	for {
		identity, ok, loginDetail := s.doLogin(ctx, conn, term, aud)
		if !ok {
			endDetail = loginDetail
			return
		}
		currentUser = identity.Username
		s.armPostAuth(conn) // authenticated: post-auth idle window

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
				if isTimeoutErr(err) {
					aud.record(ctx, store.AuditEvent{
						Kind: store.AuditLogout, Username: identity.Username, Detail: "idle logout"})
					currentUser = ""
					s.armPreAuth(conn)
					break menu
				}
				endDetail = "menu render error"
				return
			}
			if quit {
				aud.record(ctx, store.AuditEvent{
					Kind: store.AuditLogout, Username: identity.Username, Detail: "user logoff"})
				currentUser = ""
				s.armPreAuth(conn) // logoff: back to the pre-auth regime
				break menu         // logoff: back to the login screen
			}
			errMsg = ""
			if adminSel && isAdmin {
				renderer := func(conn net.Conn) ui3270.Renderer {
					if s.AdminRenderer != nil {
						return s.AdminRenderer(conn, term)
					}
					return ui3270.NewGo3270Renderer(conn, term.dev, term.codepage(), term.Rows)
				}
				flow := &adminFlow{store: s.Store, presenter: s.AdminPresenter,
					renderer: renderer,
					identity: identity, term: term, audit: aud.record}
				if aerr := flow.Run(ctx, conn); aerr != nil {
					if isTimeoutErr(aerr) {
						aud.record(ctx, store.AuditEvent{
							Kind: store.AuditLogout, Username: identity.Username, Detail: "idle logout"})
						currentUser = ""
						s.armPreAuth(conn)
						break menu
					}
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
			s.armBridge(conn)
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
			s.armPostAuth(conn) // back to the menu: restore the post-auth window
		}
	}
}

// doLogin loops the login screen until success, or returns ok=false when the
// user quits or a render error occurs. The third return value is the
// disconnect audit detail (only meaningful when ok=false); an idle-timeout
// render error is classified here so the caller audits it distinctly.
func (s *Session) doLogin(ctx context.Context, conn net.Conn, term Term, aud *auditTrail) (auth.Identity, bool, string) {
	errMsg := ""
	for {
		user, pass, quit, err := s.Presenter.Login(conn, term, errMsg)
		if err != nil {
			if isTimeoutErr(err) {
				return auth.Identity{}, false, "idle timeout"
			}
			return auth.Identity{}, false, "login render error"
		}
		if quit {
			return auth.Identity{}, false, "quit at login"
		}
		identity, err := s.Authenticate(ctx, s.Store, user, pass)
		if err == nil {
			aud.record(ctx, store.AuditEvent{Kind: store.AuditAuthOK, Username: identity.Username})
			return identity, true, ""
		}
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			// Infrastructure error (e.g. transient DB failure): log it, audit
			// it, and re-present the login screen. Do not disconnect.
			log.Printf("auth infrastructure error for user %q: %v", user, err)
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditAuthError, Username: user, Detail: err.Error()})
			errMsg = "Temporary error; try again"
			continue
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
