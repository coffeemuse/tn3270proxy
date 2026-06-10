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

// Package server accepts TCP connections and drives each one through the
// gateway's session state machine: telnet/terminal negotiation, login, an
// optional MFA gate, the MOTD/NEWS screen, the group-filtered service menu,
// and finally a bridged session to a backend (internal/bridge). PF3 steps
// back one level from every gateway screen (logoff at the menu, disconnect at
// login); PA3 during a bridge escapes back to the menu.
//
// Session (session.go) is the state machine. It depends on narrow seams
// rather than concrete implementations, so the whole flow is unit-testable
// without a live 3270 client:
//
//   - Presenter / AdminPresenter render the gateway's own screens
//     (go3270Presenter in production, fakes in tests)
//   - Bridger connects the client to a backend (realBridger)
//   - Authenticator checks credentials (auth.Authenticate)
//   - Auditor records the audit trail, best-effort (storeAuditor)
//
// Connection hardening also lives in this package: Server and ServeAll own
// the accept loops, connLimiter enforces the global and per-IP connection
// caps, idleConn enforces the idle-deadline regimes the session switches
// between (pre-auth, post-auth, bridge), authThrottle applies per-username
// backoff to failed logins, and sessionRegistry tracks live sessions for the
// admin Active Sessions screen.
//
// This package never renders screen bytes itself (internal/screens and
// internal/ui3270 do) and never speaks SQL (internal/store does).
package server
