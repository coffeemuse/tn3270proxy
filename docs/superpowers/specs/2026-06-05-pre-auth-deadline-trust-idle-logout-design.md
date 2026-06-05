# Design: pre-auth deadline + trust list + idle-logout state machine

**Issue:** [#18](https://github.com/coffeemuse/tn3270proxy/issues/18) (follow-up to #1)
**Date:** 2026-06-05
**Status:** Approved design, pre-implementation
**Closes:** #18 · **Partially addresses:** #19 (configurable bridge-idle) · **Opens follow-up:** DB/admin-UI trust management

## Problem

The #1 hardening (idle deadline + connection caps, PR #16) closed the silent-slowloris
hole, but two residual variants survive — both named in #18:

- **The trickle (the security bug).** `idleConn` re-arms `SetDeadline(now + idle)` before
  *every* Read and Write (`idleconn.go:59`), and go3270's `HandleScreen` reads byte-by-byte.
  A client sending **1 byte every ~119s** never trips the 2m pre-auth idle window, holding a
  pre-auth slot at near-zero cost. ~32 source IPs × 16/IP exhausts the 512 global cap. Because
  `connLimiter.acquire()` blocks **before** `Accept`, legitimate users then queue in the kernel
  backlog *behind* the attack — the cap converts FD exhaustion into permanent denial-at-cap.
- **Blocked-accept can't observe shutdown** (Hole B in #18). A `Serve()` goroutine parked in
  `acquire()` at full cap can't see the listener close. **Out of scope here** — the absolute
  deadline below makes held slots turn over, defusing the *permanence* of the starvation; the
  accept-loop rework (accept-then-close vs. cancellable acquire) is deferred to its own issue.

The root cause of the trickle: the per-byte idle re-arm is a **sliding** window, which silently
overwrote the **absolute** "must authenticate within N" ceiling the original #1 fix intended.

Two operational needs surfaced while scoping the fix and are folded into this design:

- **Trusted devices.** Internal dedicated devices (e.g. an OEC terminal controller that hosts
  real terminals and TN3270s into this proxy) must be able to park indefinitely at the login
  screen, and funnel **many** real terminals through **one** source IP (past the per-IP cap of 16).
- **Long-running bridged work.** Some bridged sessions run long backend processes (batch jobs,
  long queries) that should not be killed by socket idle. Operators need to choose.

## Design overview

A single connection moves through **idle regimes**. The session switches the regime at each
lifecycle transition; `idleConn` enforces whatever regime is current. Trust and the bridge-idle
policy are inputs that select which regime applies.

```
connect ─▶ PRE-AUTH ──auth ok──▶ POST-AUTH(menu/admin) ──select──▶ BRIDGE
             ▲  ▲                      │       │                      │
             │  └──── idle/PF3 logout ─┘       │                      │
             └──────────────────────── idle/PF3 logout ◀──bridge end──┘
```

- **PRE-AUTH** — idle = `PreAuthIdle` (2m) **and** a hard wall-clock ceiling `now + PreAuthMax`
  (5m). On fire: disconnect. **Trusted ⇒ exempt** (no timers; park forever). Re-entered on
  every logout, re-arming the hard ceiling fresh.
- **POST-AUTH** (menu/admin, not bridged) — idle = `Idle` (30m), no hard ceiling. On fire:
  **logout → login screen** (for *all* clients), which re-enters PRE-AUTH.
- **BRIDGE** — idle = `Idle`, **or disabled** when `bridge_idle = exempt`. On fire: disconnect.

### Why logout-on-idle instead of disconnect-on-idle (post-auth, not bridged)

Today a post-auth idle timeout makes `Session.Run` `return`, and `handle()` closes the socket
(`server.go:85`). This design instead **deauthenticates and returns to the login screen**:

1. **Stronger security** — the unattended session is explicitly logged out; the next person at
   the terminal sees a login screen, not the previous user's menu, and the event is auditable as
   a logout distinct from a disconnect.
2. **Better real-terminal UX** — the physical terminal behind a controller stays connected and
   bounces back to login rather than showing a host-disconnect state.
3. **Reuses an existing transition** — PF3-logoff at the menu already does
   `setIdle(conn, PreAuthIdle); break menu` (`session.go:166-169`). Idle-logout becomes "treat a
   post-auth idle timeout like a PF3 logoff."

## Components

### 1. `idleConn` regime enforcement (`internal/server/idleconn.go`)

Replace the single `idle` duration with `idle` + an optional `hard time.Time` ceiling. `arm()`
(called before each Read/Write) computes the deadline:

```go
func (c *idleConn) arm() {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.manual {                       // explicit deadline in force (bridge interrupt) — unchanged
        return
    }
    if c.idle <= 0 {                    // exempt / disabled regime
        c.Conn.SetDeadline(time.Time{}) // clear any prior deadline
        return
    }
    d := time.Now().Add(c.idle)
    if !c.hard.IsZero() && c.hard.Before(d) {
        d = c.hard                      // pre-auth wall-clock ceiling wins
    }
    c.Conn.SetDeadline(d)
}
```

Regime setters (replace `SetIdle`), each `mu`-guarded:

- `setPreAuth(idle, max time.Duration)` — `c.idle = idle; c.hard = time.Now().Add(max)`.
- `setExempt()` — `c.idle = 0; c.hard = time.Time{}` (trusted pre-auth).
- `setPostAuth(idle time.Duration)` — `c.idle = idle; c.hard = time.Time{}`.
- `setBridge(idle time.Duration)` — `c.idle = idle` (0 ⇒ disabled); `c.hard = time.Time{}`.

The `manual` suspend mechanism (set via `SetDeadline`/`SetReadDeadline`/`SetWriteDeadline`) is
**unchanged** — it still short-circuits `arm()` first, so the bridge teardown interrupt
(`bridge.go` sets a past deadline) is unaffected, and a zero deadline still resumes auto-arm.

`wrapIdle` always wraps when any pre-auth idle window is configured (as today); the session then
calls the regime setters. A trusted connection is wrapped too (so post-auth idle-logout still
applies), but its pre-auth regime is `setExempt()`.

### 2. Trust list (`internal/server/trust.go`, new)

```go
type trustList []netip.Prefix
func (t trustList) Contains(addr net.Addr) bool   // empty ⇒ false; unparseable IP ⇒ false
```

Built once from config and shared across listeners, exactly like `connLimiter`. Trusted-ness
selects two behaviors:

- **Pre-auth regime** ⇒ `setExempt()` instead of `setPreAuth(...)`.
- **Per-IP cap** ⇒ skipped (see component 3).

Trusted devices are **still counted toward the global cap** — the slot is claimed in `acquire()`
*before* `Accept`, before the IP is known, so this is automatic and intentional (the one backstop
a compromised trusted device must not punch through). Trusted devices **still get post-auth
idle-logout** (benign: bounces to login, where they park).

### 3. `connLimiter` per-IP exemption (`internal/server/limiter.go`, `server.go`)

`Serve()` computes trust once after `Accept`, before the per-IP check:

```go
conn, err := s.Listener.Accept()
...
trusted := s.trust.Contains(conn.RemoteAddr())
if !trusted && !s.Limiter.admitIP(conn.RemoteAddr()) {
    // reject: log, close, releaseGlobal, continue
}
go s.handle(conn, trusted)   // trusted threaded to handle so releaseIP is symmetric
```

`handle()` only calls `releaseIP` when `!trusted` (admitIP was skipped for trusted). The trusted
flag is threaded to the `connHandler` so the session can pick the pre-auth regime. To avoid
changing the `connHandler.Handle(conn)` signature, `sessionHandler` holds the same `trustList` and
re-derives trust from `conn.RemoteAddr()` (a cheap prefix scan); `Server` holds the `trustList`
for the per-IP decision. Both read one shared `trustList`.

### 4. Session state machine (`internal/server/session.go`)

New `Session` fields: `PreAuthMax time.Duration`, `Trusted bool`, `BridgeIdleExempt bool`.
Helper transitions replace the bare `setIdle` calls:

- `armPreAuth(conn)` — `Trusted ⇒ setExempt()`, else `setPreAuth(PreAuthIdle, PreAuthMax)`.
- `armPostAuth(conn)` — `setPostAuth(Idle)` (same for all clients).
- `armBridge(conn)` — `setBridge(0)` if `BridgeIdleExempt`, else `setBridge(Idle)`.

Wiring:

- **`Run` start:** `armPreAuth(conn)`.
- **Auth success:** `armPostAuth(conn)` (replaces `setIdle(conn, s.Idle)` at `session.go:146`).
- **PF3-logoff (`quit`):** `armPreAuth(conn)` + emit `AuditLogout` (detail `"user logoff"`),
  then `break menu` (replaces `setIdle(conn, s.PreAuthIdle)` at `session.go:168`).
- **Post-auth idle (menu render returns a timeout):** instead of `return`, emit `AuditLogout`
  (detail `"idle logout"`), `armPreAuth(conn)`, `break menu` to re-present login.
- **Post-auth idle (admin flow returns a timeout):** same treatment — `AuditLogout`
  (`"idle logout"`), `armPreAuth(conn)`, `break menu`. (Admin is post-auth, not bridged.)
- **Bridge start:** `armBridge(conn)`. **Bridge end** (back-to-menu causes): `armPostAuth(conn)`
  before re-rendering the menu, so the menu loop isn't left in the bridge regime.

`isTimeoutErr` (`session.go:105`) already distinguishes a net timeout from other errors; the menu
and admin error sites branch on it.

### 5. Audit (`internal/store/audit.go`)

Add `AuditLogout = "logout"`. Emitted on:

- **idle-logout** — detail `"idle logout"`.
- **PF3-logoff** — detail `"user logoff"` (closes today's gap: PF3-logoff currently emits *no*
  discrete event).

A subsequent pre-auth expiry with no re-auth still records `AuditDisconnect / "idle timeout"`
as today. The bridge-idle disconnect path is unchanged.

### 6. Config (`internal/config/config.go`, mirrored into `server.Limits`)

New `limits` keys, parsed and validated at startup (fail-fast):

| JSON key (`limits.…`) | Type | Default | Meaning |
|---|---|---|---|
| `pre_auth_max` | duration string | `"5m"` | hard wall-clock budget to authenticate; re-armed on every entry to the login screen |
| `trusted_cidrs` | `[]string` (IP or CIDR) | `[]` | clients exempt from pre-auth timers + per-IP cap; still counted toward the global cap |
| `bridge_idle` | `"disconnect"` \| `"exempt"` | `"disconnect"` | idle behavior during an active bridge |

- `config.Limits` gains `PreAuthMax time.Duration`, `TrustedCIDRs []netip.Prefix`,
  `BridgeIdleExempt bool`. `trusted_cidrs` strings are parsed to `netip.Prefix` in config
  (bare IP ⇒ `/32` or `/128`); a malformed entry is a fatal config error. `bridge_idle` must be
  one of the two literals.
- Validation: `pre_auth_max > 0`. (No ordering constraint vs `pre_auth_idle`; if the hard ceiling
  is smaller it simply dominates, which is legal.)
- `server.Limits` mirrors these (it already mirrors `config.Limits` without importing config;
  `netip`/`time` are stdlib, fine in both).
- **Scalars only get flags** (consistent with existing limits flags): `-pre-auth-max`. The
  **list and enum are config-file-only** — a CIDR list doesn't map cleanly to one flag, and these
  are deployment-static. Changes require a restart.

**Trust list lives in the static config file, not the DB / admin UI** — deliberately. It is a
security control that *bypasses* DoS protections, so it belongs on the operator/deployment surface
(root-owned file, restart to change), not a runtime surface a compromised admin account could
mutate; it is also consistent with the other `limits` knobs. A follow-up issue tracks optional
DB-backed, admin-UI-managed trust **if** runtime changes prove necessary in practice.

## Error handling

- **Deadline fires mid-Read at the login screen (pre-auth):** `Presenter.Login` returns a net
  timeout → `doLogin` returns `"idle timeout"` → `Run` returns → `handle()` closes. Unchanged
  path; this is the non-trusted "didn't authenticate in time" disconnect.
- **Deadline fires mid-Read post-auth:** caught at the menu/admin site → logout (above), not a
  disconnect.
- **Malformed `trusted_cidrs` / bad `bridge_idle`:** fatal at config load — the process never
  starts with an unparseable trust/policy surface.
- **Trusted device whose bridged session idles** (`bridge_idle = disconnect`): disconnects like
  any client — trust does not extend into the bridge regime. In practice a chatty backend
  refreshes the socket so it rarely fires (a #19 nuance noted, not solved here).

## Testing

TDD per CLAUDE.md; `go test ./... -race`. Timing tests follow the existing `idleconn_test.go`
pattern (short real durations, no fake clock).

- **`idleconn_test.go`:** hard ceiling clamps below the idle window (fires at `hard`, not
  `now+idle`); `setExempt`/`setBridge(0)` clears the deadline (no fire); `setPreAuth` re-arm gives
  a fresh ceiling; `manual` suspend still wins over a pending regime.
- **`trust_test.go` (new):** `Contains` for exact IP, CIDR membership, non-member, IPv6, empty
  list, unparseable addr.
- **`limiter_test.go`:** trusted IP bypasses the per-IP cap; still consumes (and releases) a
  global slot; non-trusted unaffected.
- **`session_*_test.go`:** post-auth menu idle → login re-presented (fake Presenter returns one
  timeout) + `AuditLogout`; admin-flow idle → login re-presented + `AuditLogout`; PF3-logoff emits
  `AuditLogout`; trusted session uses exempt pre-auth; `BridgeIdleExempt` disables the bridge
  timeout; bridge-end restores post-auth idle.
- **`config_test.go`:** parse/validate `pre_auth_max`, `trusted_cidrs` (good + malformed),
  `bridge_idle` (good + invalid); `-pre-auth-max` flag override.
- **s3270 smoke (the protocol guard):** with a short configured `idle`, drive login → menu, wait
  out the window, and assert the screen **cleanly resets to the login screen** with the cursor
  homed correctly (the field-attribute / `HandleScreen`-after-timeout behavior unit tests can't
  prove). Re-render after a deadline that fired mid-`HandleScreen` is the known-risky path
  (CLAUDE.md "learned the hard way").

## Docs

- **CLAUDE.md:** add the three knobs to the `internal/config` and `internal/server` summaries;
  add the regime table and the logout-on-idle behavior to the hardening notes; note the trust
  list is config-file-only by design.
- **#19:** comment that bridge-idle now has a documented, configurable answer
  (`disconnect`/`exempt`); true *user-idle* during bridge (client-only re-arm) remains its open item.
- **New follow-up issue:** DB-backed / admin-UI-managed trust list (deferred).

## Out of scope

- Accept-loop rework (Hole B: accept-then-close vs. cancellable `acquire`, and the shutdown
  observability gap) — its own issue.
- True user-idle during bridge (client-only re-arm) — stays in #19.
- DB/admin-UI trust management — follow-up issue.
