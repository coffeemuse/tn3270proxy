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
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/auth"
	"github.com/CoffeeMuse/tn3270proxy/internal/bridge"
	"github.com/CoffeeMuse/tn3270proxy/internal/mfa"
	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/CoffeeMuse/tn3270proxy/internal/sysconfig"
	"github.com/CoffeeMuse/tn3270proxy/internal/ui3270"
)

// Presenter renders the proxy's own 3270 screens to the client. The real
// implementation wraps go3270; tests use a fake. The Term returned by
// Negotiate must be passed back into every subsequent call so screens render
// at the client's negotiated size and codepage.
type Presenter interface {
	Negotiate(conn net.Conn) (Term, error)
	Login(conn net.Conn, term Term, errMsg string) (username, password string, quit bool, err error)
	Menu(conn net.Conn, term Term, services []store.Service, admin bool, status screens.MenuStatus, errMsg string) (selected *store.Service, adminSel bool, quit bool, err error)
	// News shows the MOTD pages (already paginated) one at a time: ENTER
	// advances, the last ENTER returns nil. PA3/PF3 are silent no-ops. A
	// non-nil error is a disconnect or an idle timeout (classified by the
	// caller). News is only called with at least one page.
	News(conn net.Conn, term Term, pages [][]string) error
	// EnrollMFA shows the chunked secret and prompts for a confirmation code.
	// Returns the entered code; quit=true on PF3 (cancel to login).
	EnrollMFA(conn net.Conn, term Term, issuer, account, chunkedSecret, errMsg string) (code string, quit bool, err error)
	// VerifyMFA prompts an enrolled user for a code. quit=true on PF3.
	VerifyMFA(conn net.Conn, term Term, errMsg string) (code string, quit bool, err error)
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
	// RemoteHost is the client IP (port stripped) for the fail2ban <HOST> on
	// auth-failure log lines (see logAuthFailure). Set by the handler from the
	// connection's RemoteAddr; empty in unit tests that construct Session
	// directly unless set explicitly.
	RemoteHost string
	// MOTDRead reads the MOTD file for maybeShowNews; nil selects the capped
	// os.ReadFile default (readMOTDCapped). Tests inject a fake.
	MOTDRead func(path string) ([]byte, error)
	// Logger is the per-connection structured logger. nil falls back to
	// slog.Default(). The session enriches it with "user" after authentication.
	Logger *slog.Logger
	// Release is the resolved build version shown in the menu status block
	// (version.Resolve, threaded from cmd via NewSessionHandler). Empty renders
	// a blank Release row.
	Release string
	// MFA encrypts/decrypts TOTP secrets; nil disables MFA globally (the gate is
	// skipped). Set from the resolved master key by the handler.
	MFA *mfa.Cipher
	// Now returns the current time for TOTP validation; nil → time.Now.
	Now func() time.Time
	// MFAGenerate creates a new TOTP secret; nil → mfa.GenerateSecret. Injected
	// in tests so enrollment is deterministic.
	MFAGenerate func(issuer, account string) (string, error)
	// Throttle applies per-username backoff to failed auth attempts (GH #48);
	// nil disables throttling (no delay). Shared across sessions by the handler.
	Throttle *authThrottle
	// Sleep delays the next prompt after a failed attempt; nil → time.Sleep.
	// Tests inject a recorder to assert the computed delay without waiting.
	Sleep func(time.Duration)
}

func (s *Session) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Session) generateSecret(issuer, account string) (string, error) {
	if s.MFAGenerate != nil {
		return s.MFAGenerate(issuer, account)
	}
	return mfa.GenerateSecret(issuer, account)
}

// mfaIssuer reads the configured issuer label, falling back to a constant.
func (s *Session) mfaIssuer(ctx context.Context) string {
	v, err := s.Store.GetConfig(ctx, sysconfig.KeyMFAIssuer)
	if err != nil || strings.TrimSpace(v) == "" {
		return "TN3270PROXY"
	}
	return v
}

// systemID reads the configured System ID for the menu status block, falling
// back to "PROXY" on any read error or empty value so the menu never renders a
// blank System ID row.
func (s *Session) systemID(ctx context.Context) string {
	v, err := s.Store.GetConfig(ctx, sysconfig.KeySystemID)
	if err != nil || strings.TrimSpace(v) == "" {
		return "PROXY"
	}
	return v
}

// log returns the session's logger (slog.Default() when Logger is nil).
func (s *Session) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// logAuthFailure emits the stable, fail2ban-friendly auth-failure line. The
// field set — msg="auth failed" plus src, trusted, reason, and the user carried
// by lg — is a DOCUMENTED STABLE CONTRACT (operators build log filters against
// it); do not rename or drop fields without updating README and the shape test.
// lg must already carry the attempted username as "user": pre-auth callers pass
// s.log().With("user", user); post-auth callers pass s.log() (already enriched).
// reason is coarse ("invalid_credentials" or "bad_mfa") so it never reveals
// whether a username exists. Never pass credential content (password / TOTP
// code / MFA secret).
func (s *Session) logAuthFailure(lg *slog.Logger, reason string) {
	lg.Warn("auth failed",
		"src", s.RemoteHost,
		"trusted", s.Trusted,
		"reason", reason,
	)
}

// motdReadCap bounds how much of the MOTD file is read. A legitimate notice is
// a few screens of text; the cap is a defensive ceiling against a misconfigured
// path (a device/FIFO or a huge file). An over-cap file is truncated, not
// rejected — the banner is simply clipped.
const motdReadCap = 8 << 10 // 8 KiB

// readMOTDCapped is the default MOTDRead: it reads at most motdReadCap bytes.
func readMOTDCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, motdReadCap))
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
		s.log().Error("negotiation failed", "error", err)
		endDetail = "negotiation failed"
		if isTimeoutErr(err) {
			endDetail = "idle timeout"
		}
		return
	}

	// baseLog is the pre-auth logger (tagged with "remote"); after login the
	// session enriches it with "user". On logoff the session reverts to baseLog.
	baseLog := s.log()

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
		s.Logger = baseLog.With("user", identity.Username) // enrich with user
		s.armPostAuth(conn)                                // authenticated: post-auth idle window

		// MFA gate: enrollment (pending) or verification (enrolled) before the menu.
		proceed, mfaDetail, ferr := s.mfaGate(ctx, conn, term, identity, aud)
		if ferr != nil {
			endDetail = mfaDetail
			return
		}
		if !proceed { // idled out or cancelled during MFA: back to the login screen
			currentUser = ""
			s.Logger = baseLog
			continue
		}

		// MOTD/NEWS gate: shown once per login, before the menu.
		enterMenu, nerr := s.maybeShowNews(ctx, conn, term, identity, aud)
		if nerr != nil {
			endDetail = "news render error"
			return
		}
		if !enterMenu { // idled out during the gate: back to the login screen
			currentUser = ""
			s.Logger = baseLog // revert to pre-user logger
			continue
		}

		isAdmin := s.AdminPresenter != nil && slices.Contains(identity.Groups, store.AdminGroup)
		errMsg := ""
	menu:
		for {
			services, err := s.Store.ListServicesForGroups(ctx, identity.Groups)
			if err != nil {
				s.log().Error("list services failed", "error", err)
				services = nil
				errMsg = "Temporary error retrieving services; try again"
			}
			status := screens.MenuStatus{
				Username: identity.Username,
				SystemID: s.systemID(ctx),
				Release:  s.Release,
			}
			selected, adminSel, quit, err := s.Presenter.Menu(conn, term, services, isAdmin, status, errMsg)
			if err != nil {
				if isTimeoutErr(err) {
					aud.record(ctx, store.AuditEvent{
						Kind: store.AuditLogout, Username: identity.Username, Detail: "idle logout"})
					currentUser = ""
					s.Logger = baseLog // revert to pre-user logger
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
				s.Logger = baseLog // revert to pre-user logger
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
					identity: identity, term: term, audit: aud.record,
					logger: s.log()}
				if aerr := flow.Run(ctx, conn); aerr != nil {
					if isTimeoutErr(aerr) {
						aud.record(ctx, store.AuditEvent{
							Kind: store.AuditLogout, Username: identity.Username, Detail: "idle logout"})
						currentUser = ""
						s.Logger = baseLog // revert to pre-user logger
						s.armPreAuth(conn)
						break menu
					}
					s.log().Error("admin flow error", "error", aerr)
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
				s.log().Error("bridge error", "service", selected.Name, "addr", addr, "error", berr)
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

// maybeShowNews renders the MOTD/NEWS gate once after login, before the menu.
// It returns (true, nil) to proceed into the menu — including every skip case
// (MOTD disabled, unreadable, relative path, or empty). It returns (false, nil)
// when the user idled out during the gate (audited + pre-auth re-armed here; the
// caller returns to the login screen). A non-nil error is a fatal render error
// (the caller disconnects).
func (s *Session) maybeShowNews(ctx context.Context, conn net.Conn, term Term, identity auth.Identity, aud *auditTrail) (bool, error) {
	path, err := s.Store.GetConfig(ctx, sysconfig.KeyMOTDFile)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log().Warn("MOTD config key unreadable; skipping", "error", err)
		}
		return true, nil
	}
	if strings.TrimSpace(path) == "" {
		return true, nil // disabled → straight to the menu
	}
	if !filepath.IsAbs(path) {
		s.log().Warn("MOTD path not absolute; skipping", "path", path)
		return true, nil
	}
	read := s.MOTDRead
	if read == nil {
		read = readMOTDCapped
	}
	data, err := read(path)
	if err != nil {
		s.log().Warn("MOTD file unreadable; skipping", "path", path, "error", err)
		return true, nil
	}
	pages := screens.PaginateNews(term.Geometry(), string(data))
	if len(pages) == 0 {
		return true, nil // empty/whitespace-only → straight to the menu
	}
	if err := s.Presenter.News(conn, term, pages); err != nil {
		if isTimeoutErr(err) {
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditLogout, Username: identity.Username, Detail: "idle logout"})
			s.armPreAuth(conn)
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// mfaGate runs the MFA step after a successful login. It returns proceed=true
// to continue to the MOTD/menu. proceed=false means the user idled out or
// cancelled (PF3) during MFA — the pre-auth regime is re-armed here and the
// caller returns to the login screen. A non-nil error is fatal (disconnect),
// with detail set for the disconnect audit.
func (s *Session) mfaGate(ctx context.Context, conn net.Conn, term Term, identity auth.Identity, aud *auditTrail) (proceed bool, detail string, err error) {
	if s.MFA == nil {
		return true, "", nil // MFA disabled globally
	}
	u, gerr := s.Store.GetUserByUsername(ctx, identity.Username)
	if gerr != nil {
		s.log().Error("mfa: load user failed", "error", gerr)
		return false, "mfa user load error", gerr
	}
	if !u.MFARequired {
		return true, "", nil
	}
	if u.MFASecret == "" {
		return s.mfaEnroll(ctx, conn, term, u, aud)
	}
	return s.mfaVerify(ctx, conn, term, u, aud)
}

func (s *Session) mfaEnroll(ctx context.Context, conn net.Conn, term Term, u store.User, aud *auditTrail) (bool, string, error) {
	issuer := s.mfaIssuer(ctx)
	secret, gerr := s.generateSecret(issuer, u.Username)
	if gerr != nil {
		s.log().Error("mfa: generate secret failed", "error", gerr)
		return false, "mfa generate error", gerr
	}
	chunked := mfa.Chunk(secret)
	errMsg := ""
	for {
		code, quit, err := s.Presenter.EnrollMFA(conn, term, issuer, u.Username, chunked, errMsg)
		if err != nil {
			if isTimeoutErr(err) {
				aud.record(ctx, store.AuditEvent{Kind: store.AuditLogout, Username: u.Username, Detail: "idle logout"})
				s.armPreAuth(conn)
				return false, "", nil
			}
			return false, "mfa render error", err
		}
		if quit {
			s.armPreAuth(conn)
			return false, "", nil
		}
		ok, step, verr := mfa.Validate(secret, code, 0, s.now())
		if verr != nil {
			s.log().Error("mfa: validate error", "error", verr)
			errMsg = "Temporary error; try again"
			continue
		}
		if !ok {
			s.logAuthFailure(s.log(), "bad_mfa")
			delay, count := s.failDelay(ctx, u.Username)
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditMFAFailed, Username: u.Username, Detail: throttleDetail("enroll", delay, count)})
			s.sleepFor(delay)
			errMsg = "Code incorrect - check the key and try again"
			continue
		}
		enc, serr := s.MFA.Seal([]byte(secret))
		if serr != nil {
			return false, "mfa seal error", serr
		}
		enrolledAt := s.now().UTC().Format(time.RFC3339)
		if err := s.Store.StoreMFAEnrollment(ctx, u.ID, enc, enrolledAt, int64(step)); err != nil {
			return false, "mfa store error", err
		}
		s.Throttle.Reset(u.Username)
		aud.record(ctx, store.AuditEvent{Kind: store.AuditMFAEnrolled, Username: u.Username})
		return true, "", nil
	}
}

func (s *Session) mfaVerify(ctx context.Context, conn net.Conn, term Term, u store.User, aud *auditTrail) (bool, string, error) {
	pt, oerr := s.MFA.Open(u.MFASecret)
	if oerr != nil {
		s.log().Error("mfa: secret decrypt failed", "user", u.Username, "error", oerr)
		return false, "mfa decrypt error", oerr
	}
	secret := string(pt)
	errMsg := ""
	for {
		code, quit, err := s.Presenter.VerifyMFA(conn, term, errMsg)
		if err != nil {
			if isTimeoutErr(err) {
				aud.record(ctx, store.AuditEvent{Kind: store.AuditLogout, Username: u.Username, Detail: "idle logout"})
				s.armPreAuth(conn)
				return false, "", nil
			}
			return false, "mfa render error", err
		}
		if quit {
			s.armPreAuth(conn)
			return false, "", nil
		}
		ok, step, verr := mfa.Validate(secret, code, uint64(u.MFALastStep), s.now())
		if verr != nil {
			s.log().Error("mfa: validate error", "error", verr)
			errMsg = "Temporary error; try again"
			continue
		}
		if !ok {
			s.logAuthFailure(s.log(), "bad_mfa")
			delay, count := s.failDelay(ctx, u.Username)
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditMFAFailed, Username: u.Username, Detail: throttleDetail("login", delay, count)})
			s.sleepFor(delay)
			errMsg = "Code incorrect - try again"
			continue
		}
		if err := s.Store.UpdateMFAStep(ctx, u.ID, int64(step)); err != nil {
			return false, "mfa store error", err
		}
		s.Throttle.Reset(u.Username)
		aud.record(ctx, store.AuditEvent{Kind: store.AuditMFASuccess, Username: u.Username})
		return true, "", nil
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
			s.Throttle.Reset(user)
			aud.record(ctx, store.AuditEvent{Kind: store.AuditAuthOK, Username: identity.Username})
			return identity, true, ""
		}
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			// Infrastructure error (e.g. transient DB failure): log it, audit
			// it, and re-present the login screen. Do not disconnect.
			// Username only — never the password (CLAUDE.md hard rule).
			s.log().Error("auth error", "user", user, "error", err)
			aud.record(ctx, store.AuditEvent{
				Kind: store.AuditAuthError, Username: user, Detail: err.Error()})
			errMsg = "Temporary error; try again"
			continue
		}
		// Auth fail: per-username backoff (GH #48). Compute before auditing so
		// the audit detail records the applied delay; never log the password.
		delay, count := s.failDelay(ctx, user)
		// Auth fail: stable fail2ban line — attempted username only, never the
		// password (CLAUDE.md hard rule). Coarse reason: no enumeration leak.
		s.logAuthFailure(s.log().With("user", user), "invalid_credentials")
		aud.record(ctx, store.AuditEvent{
			Kind: store.AuditAuthFail, Username: user, Detail: throttleDetail("", delay, count)})
		s.sleepFor(delay) // bounded well under pre_auth_idle by default; a tight pre-auth window could turn a large delay into a timeout
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
