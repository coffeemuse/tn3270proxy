# TN3270Proxy — Roadmap (remaining goals)

The MVP (connect → login → group-filtered menu → bridge → PA3-return) is complete and on
`main`. This document captures the deferred follow-on milestones (spec §9) with enough
context to **start any of them in a fresh session**: the goal, where it hooks into the
existing code, design considerations, a suggested approach, and dependencies.

**Process reminder:** each milestone below is its own brainstorm → spec → plan →
implementation cycle, same as the MVP. New specs go in `docs/superpowers/specs/`, plans in
`docs/superpowers/plans/`. See `CLAUDE.md` for architecture and conventions.

Suggested order (rationale in each section): **1 → 2 → 5 → 3 → 4 → 6**, i.e. harden the
public edge first (TLS in, then TLS out, then audit), then build admin tooling, then protocol
breadth, then scale.

---

## 1. TLS-terminated inbound listener  *(highest priority — it's public-facing)*

**Goal:** Accept TLS connections from clients (TN3270 over TLS, sometimes "TN3270 Secure"),
not just plaintext. This is the gating item for any real public exposure.

**Where it hooks in:**
- `cmd/tn3270proxy/runServe` creates the listener: `net.Listen("tcp", cfg.ListenAddr)`.
  Wrap with `tls.NewListener(ln, tlsConfig)` when TLS is configured.
- `internal/config`: add `TLSCert`, `TLSKey` (and maybe `TLSListenAddr` if serving both
  plaintext and TLS on different ports). Keep `Load` flag-based.
- `internal/server/server.go` is transport-agnostic (`net.Listener`), so it needs **no
  change** — a `*tls.Conn` is a `net.Conn`. go3270's `NegotiateTelnet`/`HandleScreen` and the
  bridge all take `net.Conn`, so they work unchanged over TLS.

**Design considerations:**
- TN3270 emulators expect **immediate TLS** on connect (not STARTTLS). `tls.NewListener` does
  exactly that.
- Cert loading: `tls.LoadX509KeyPair`. Decide cert-reload story (probably out of scope — load
  at startup).
- Consider whether to keep a plaintext listener at all (maybe gate behind a flag; default to
  TLS-only once certs are configured).

**Suggested approach:** Add config fields + a helper that returns the `net.Listener`
(plaintext or TLS) based on config; everything downstream is untouched. Test with a self-signed
cert + `crypto/tls` client dialing the listener and completing go3270 negotiation.

**Dependencies:** none. Do this first.

---

## 2. Backend-side TLS dialing  *(reserved hook already exists)*

**Goal:** Connect to backend services over TLS when the service is marked TLS. The
`services.tls` column **already exists** and is plumbed through `store.Service.TLS`, the seed
format (`SeedService.TLS`), and the menu — but the bridge ignores it.

**Where it hooks in:**
- `internal/bridge/bridge.go`, `Bridge(...)`: currently always
  `net.DialTimeout("tcp", addr, dialTimeout)`. Needs a TLS variant.
- `internal/server/session.go`, `Session.Run`: it picks the `*store.Service` and calls
  `s.Bridger.Bridge(conn, addr, termType, escapeAID)` — the `TLS bool` is available on
  `selected` but not currently passed. **The `Bridger` interface signature will need to carry
  TLS** (e.g. add a `tls bool` param, or pass the whole `store.Service`).
- `internal/server/presenter.go`, `realBridger.Bridge`: update to match.

**Design considerations:**
- This changes the `Bridger` interface → update `fakeBridger` in `session_test.go` too.
- TLS to the backend usually needs server-name / verification policy. For internal hosts you
  may need `InsecureSkipVerify` or a custom CA bundle — make it a deliberate config decision,
  not a silent skip. Document it.
- The Telnet-aware relay logic is unchanged; only the dial differs (`tls.DialWithDialer` /
  `tls.Client` over the TCP conn).

**Suggested approach:** Widen the `Bridger` interface to take the service's TLS flag (or the
`store.Service`); branch the dial in `bridge.Bridge`. Add a fake-backend TLS test mirroring the
existing `TestBridgeNegotiatesAndRelays`.

**Dependencies:** independent of #1, but naturally paired with it.

---

## 3. Admin management UI for users / groups / services

**Goal:** Manage users, groups, services, and their links without hand-editing JSON + re-seed.
Today the only path is `seed -file`.

**Where it hooks in:**
- `internal/store` already has all the CRUD primitives (`CreateUser`, `CreateGroup`,
  `AddUserToGroup`, `CreateService`, `LinkGroupService`, plus the read queries). You'll likely
  need **delete/update** methods (don't exist yet) and listing methods (e.g. `ListUsers`,
  `ListGroups`, `ListAllServices`).
- Decision: **what kind of UI?** Options, in rough effort order:
  1. A richer CLI (`tn3270proxy admin user add/list/rm`, etc.) — smallest step, reuses store.
  2. A 3270 admin screen set (consistent with the product; reuse `internal/screens` + go3270,
     gate behind an "admin" group via the existing group model). Fits the existing session
     machine — could be a special menu entry visible only to an admin group.
  3. A small HTTP admin (separate `net/http` listener) — most flexible, but adds a web surface
     to a security-sensitive box (think about auth + binding to localhost only).

**Design considerations:**
- Whatever the surface, **reuse `internal/store`** as the single source of truth; don't add a
  second data path.
- Password *update* is needed (the MVP's `CreateUser` is idempotent and does NOT change an
  existing hash — see `seed` notes). Add `SetPassword`/`UpdateUser`.
- If 3270-based, an admin is just a user in an `admin` group; the session machine can branch to
  an admin menu. This is the most on-brand option.

**Suggested approach:** Start with the CLI (#1) for fast value, then optionally the 3270 admin
screens (#2). Brainstorm the surface choice explicitly before planning.

**Dependencies:** none functionally, but more useful after #1/#2 so admins can manage TLS
services.

---

## 4. TN3270E support

**Goal:** Negotiate TN3270E (RFC 2355) in addition to base TN3270 — device-type/LU naming,
BIND, response handling, structured fields.

**Where it hooks in:**
- Inbound: depends on what `go3270` supports. **Verify go3270's TN3270E capability first**
  (the MVP uses base TN3270; go3270 docs didn't clearly claim TN3270E). If go3270 lacks it,
  inbound TN3270E is a large effort or a library change.
- Backend leg: `internal/bridge/telnet.go` `telnetProcessor` would need to negotiate the
  TN3270E Telnet option (option 40) and handle the TN3270E 5-byte data-stream header that
  prefixes each record. The current relay forwards raw 3270 records framed by `IAC EOR`; under
  TN3270E the framing/headers differ.

**Design considerations:**
- This is the **largest and least-bounded** item. Scope it carefully; consider whether real
  target hosts even require it (many accept base TN3270).
- Both legs must agree on TN3270E vs base — the proxy must negotiate consistently on inbound
  and backend legs, like it already echoes terminal type.

**Suggested approach:** Spike go3270's TN3270E support before committing to a plan. May warrant
its own design doc on the data-stream header handling in the bridge.

**Dependencies:** build on the stable bridge; do after the edge is hardened.

---

## 5. Audit logging

**Goal:** Durable, queryable record of who connected, when, from where, which service they
reached, and session outcomes — important for a public-facing access gateway.

**Where it hooks in:**
- `internal/server/session.go` is the natural choke point: it sees the authenticated
  `Identity`, the selected service, and the bridge `Cause` for each session. Today it only does
  ad-hoc `log.Printf`.
- `internal/server/server.go` `handle` sees accept/close and `conn.RemoteAddr()`.
- Auth events (success/failure) live in `internal/auth` / `doLogin`.

**Design considerations:**
- Decide the sink: structured logs (JSON via `slog`) vs an `audit` table in the same SQLite DB
  vs both. An `audit` table fits the existing store and is queryable.
- Events to capture: connect, auth success/failure (username + source IP, never password),
  service selected, bridge start/end + cause, disconnect.
- Privacy/security: never log credentials; consider log rotation/retention.

**Suggested approach:** Define an `Auditor` interface (mirrors the `Presenter`/`Bridger` seam
pattern) injected into `Session`; provide a store-backed impl (new `audit` table + `store`
methods) and a no-op for tests. This keeps the session testable and the data path single-sourced.

**Dependencies:** light; pairs well with #1 (you want audit before going public).

---

## 6. Connection pooling / session multiplexing / load balancing

**Goal:** Scale beyond one-goroutine-per-connection: pool/reuse backend connections, balance
across multiple backend hosts for a logical service, manage many concurrent sessions efficiently.

**Where it hooks in:**
- `internal/server/server.go` already spawns a goroutine per accepted conn (fine for moderate
  load). Multiplexing would change how backend connections are acquired in `internal/bridge`.
- `store.Service` is currently one host:port. Load balancing implies multiple endpoints per
  service (schema change: a `service_endpoints` table, or repeated services) + selection policy.

**Design considerations:**
- This is a **scale** concern; only pursue when load justifies it. Most mainframe sessions are
  long-lived and stateful, so "pooling" backend conns is subtle (you generally can't share a
  live 3270 session across users). More realistic near-term value: per-service endpoint
  selection / failover.
- Don't over-build. Revisit the actual concurrency target before designing.

**Suggested approach:** Defer until there's a real load requirement. When needed, start with
endpoint selection/failover (schema + selection policy in the session) before true pooling.

**Dependencies:** last; depends on real-world load data.

---

## Quick reference: what's already wired for the future

| Future need | Existing hook |
|---|---|
| Backend TLS | `services.tls` column → `store.Service.TLS` → `SeedService.TLS` (plumbed; bridge ignores it) |
| Configurable escape key | `Session.EscapeAID` / `bridge.EscapeAIDPA3` (hard-coded to PA3 at wiring) |
| Alternate transport (TLS in) | `Server` is `net.Listener`-based; all layers take `net.Conn` |
| Admin via 3270 | group model + session machine; an "admin" group + admin menu branch |
| Audit | `Session.Run` sees Identity + service + bridge Cause; add an `Auditor` seam |
| Pluggable identity source | `auth.UserStore` interface already abstracts the store (LDAP later = new impl) |
