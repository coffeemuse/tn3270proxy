# TN3270Proxy — Roadmap (remaining goals)

The MVP (connect → login → group-filtered menu → bridge → PA3-return) is complete and on
`main`. This document captures the deferred follow-on milestones (spec §9) with enough
context to **start any of them in a fresh session**: the goal, where it hooks into the
existing code, design considerations, a suggested approach, and dependencies.

**Process reminder:** each milestone below is its own brainstorm → spec → plan →
implementation cycle, same as the MVP. New specs go in `docs/superpowers/specs/`, plans in
`docs/superpowers/plans/`. See `CLAUDE.md` for architecture and conventions.

**Progress:** Milestones **1, 2, and 3 are complete** (3 merged to `main`; live smoke test
passed 2026-06-03). Smoke testing #3 surfaced UX refinements, slated as **#3.5**. Remaining
order: **3.5 → 5 → 7 → 4 → 6** — admin/menu UX polish first (small), then audit logging
(still wanted before going public), larger terminals (7), protocol breadth, scale.
**Next up: #3.5 (admin/menu UX refinements).**

**Carried-over debt:** #2's manual live smoke test (real emulator + TLS TN3270 backend; see
the checklist at the end of `docs/superpowers/plans/2026-06-03-backend-tls-dialing.md`) has
NOT been run yet — no TLS backend was available. Run it when one exists.

---

## 1. TLS-terminated inbound listener  ✅ **DONE** *(merged to `main`)*

> **Completed.** Spec: `docs/superpowers/specs/2026-06-03-tls-inbound-listener-design.md`;
> plan: `docs/superpowers/plans/2026-06-03-tls-inbound-listener.md`. Delivered: a JSON config
> file (`tn3270proxy.json`, default-discovered) with independent `plain`/`tls` listeners and
> `defaults < file < flags` precedence (strict, unknown-key-rejecting); `internal/listen.Build`
> (immediate TLS via `tls.NewListener`, TLS 1.2 floor); `server.ServeAll` (one accept loop per
> listener sharing a handler, race-clean teardown-on-error). Backward compatible (no config →
> plaintext `:2323`). The historical notes below are retained for reference.

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

## 2. Backend-side TLS dialing  ✅ **DONE** *(complete; see spec/plan)*

> **Completed.** Spec: `docs/superpowers/specs/2026-06-03-backend-tls-dialing-design.md`;
> plan: `docs/superpowers/plans/2026-06-03-backend-tls-dialing.md`. Delivered: a per-service
> `tls_verify` column (default on; guarded idempotent migration), `verify` in the seed format
> (`*bool`, omitted → on), `bridge.Bridge(..., *tls.Config)` (nil = plaintext), and a
> `server.BackendTLS{Enabled,Verify}` intent on the `Bridger` seam translated by
> `backendTLSConfig` (TLS 1.2 floor, ServerName = configured host, system-root verification;
> `Verify=false` → encrypted-but-unauthenticated). Deferred: CA bundles, ServerName override,
> mTLS. The historical notes below are retained for reference.

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

## 3. Admin management UI for users / groups / services  ✅ **DONE** *(merged to `main`; smoke test passed)*

> **Completed.** Spec: `docs/superpowers/specs/2026-06-03-admin-ui-design.md`;
> plan: `docs/superpowers/plans/2026-06-03-admin-ui.md`. Delivered: ZZADMIN reserved group
> (ZZ* namespace, case-insensitive `store.ReservedGroupPrefix`; ZZADMIN auto-created in
> `migrate()`), `A` menu entry visible only to ZZADMIN members, full CRUD via ISPF-style
> screens (paging PF7/PF8, line commands, delete confirm round-trip) for users, groups,
> services, and memberships/access links; guardrails (no self-delete, last-admin guard,
> ZZ* create/delete blocked); store list/update/cascade-delete/count methods;
> `auth.HashPassword` as the single bcrypt path (seed + admin UI); generic
> `AdminListScreen`/`AdminFormScreen` builders in `internal/screens`. The historical notes
> below are retained for reference.

**Goal:** Manage users, groups, services, and their links without hand-editing JSON + re-seed.
Today the only path is `seed -file`.

**Where it hooks in:**
- `internal/store` already has all the CRUD primitives (`CreateUser`, `CreateGroup`,
  `AddUserToGroup`, `CreateService` — now 6-arg: `(ctx, name, host, port, tls, verify)` since
  milestone #2 — `LinkGroupService`, plus the read queries). You'll likely need
  **delete/update** methods (don't exist yet) and listing methods (e.g. `ListUsers`,
  `ListGroups`, `ListAllServices`).
- Since #2, services carry **two TLS fields** (`tls`, `tls_verify`); any admin create/update
  surface must expose both. Remember all `Create*` are INSERT OR IGNORE — they do **not**
  update existing rows (that's exactly why update methods are needed; same root cause as the
  password-update note below).
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

## 3.5 Admin / menu UX refinements

**Goal:** Polish items surfaced by the #3 live smoke test (2026-06-03). Three changes,
all small, all in the session/adminFlow/screens layer — no schema work.

1. **Group → members view.** From the admin GROUPS list, a line command (e.g. `S` or `M`)
   on a group opens a members screen for that group, instead of only showing a count.
   *Suggested shape:* reuse the existing toggle-list pattern (`userGroups`/`serviceGroups`):
   list all users with an `X` marker for members, `A`/`R` to add/remove — membership becomes
   manageable from either side. The last-ZZADMIN guard (`guardLastAdmin`) already applies.
   Store may want `ListUsersInGroup(ctx, gid)` (or reuse `ListUsers`+`GetUserGroups`).

2. **Drop PA3 from the admin screens.** PA3's product meaning is "escape the remote host"
   (bridge escape); having it also mean "jump to service menu" inside admin screens is
   confusing. Remove PA3 from the admin exit keys and rely on PF3 walking up one level at a
   time. *Bonus:* this deletes the `bail bool` plumbing threaded through every adminFlow
   method and `adminListExitKeys`/form exit keys lose `AIDPA3`. PA3 stays bridge-only.

3. **Layered PF3 logout.** PF3 from the service menu logs out (back to the login screen)
   instead of disconnecting; PF3 from the login screen disconnects. `Session.Run` loops
   from menu-quit back to `doLogin` instead of returning. Update help-line texts
   ("PF3 = logoff" on the menu). Note: a disconnect from the menu becomes two PF3 presses —
   standard mainframe layering. Re-login re-evaluates groups (nice side effect: a demoted
   admin loses the `A` entry at logout, partially addressing the live-effect caveat).

   **Decided (2026-06-03):** PF3 is uniformly "back to the previous level" across the whole
   UI: admin sub-screen → admin menu → service menu → login screen → disconnect. PF3 from
   the *admin menu* therefore goes to the service menu (unchanged from today); only the
   service-menu and login-screen PF3 behaviors change.

**Where it hooks in:** `internal/server/session.go` (Run loop), `internal/server/admin*.go`
(exit-key handling, bail plumbing removal), `internal/server/presenter*.go` (exit keys),
`internal/screens` (help-line texts, members screen via existing `AdminListScreen`).

**Dependencies:** none; do before #5 so audit logging (#5) captures the final
login/logout/disconnect event shapes.

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

## 7. Support larger terminal models (MOD 3 / MOD 4 / MOD 5)

**Goal:** Render the login and menu screens correctly on terminals larger than the 24×80
default (MOD 2). The bottom-anchored elements (error line, PF-key help) must move to the
actual last rows, and the menu should use the extra rows to list more services.

3270 model geometries (rows × cols): **MOD 2 = 24×80** (current hard-coded assumption),
**MOD 3 = 32×80**, **MOD 4 = 43×80**, **MOD 5 = 27×132**. Today the screens place the error
line at row 21 and the PF help at row 23 — correct only for a 24-row screen. On a MOD 3/4 the
help line would float in the middle with empty rows below it.

**Where it hooks in:**
- `internal/screens/login.go` and `menu.go` hard-code row numbers (title 0; error 21; PF 23;
  menu selection line 19; list rows 4..). These builders must take the screen **dimensions**
  (at least row count) and compute bottom-anchored positions: PF help = `rows-1`, error =
  `rows-3` (preserving the "error just above the help line" convention from `CLAUDE.md`),
  selection line below the list, etc. The menu's list capacity grows with `rows`.
- `internal/server/presenter.go`: `Negotiate` currently calls `go3270.NegotiateTelnet(conn)`
  and **discards everything but `TerminalType()`**. go3270's `DevInfo` also exposes
  `AltDimensions() (rows, cols int)` and there is a `go3270.HandleScreenAlt(..., dev DevInfo)`
  variant for non-24×80 screens. Switch Login/Menu to `HandleScreenAlt` and feed the alternate
  dimensions into the screen builders.
- `internal/server/session.go`: the `Presenter` interface and the presenter's per-connection
  state need the dimensions. Options: (a) have `Negotiate` return the `DevInfo` (or a small
  `Dimensions` struct) and thread it through `Login`/`Menu`; or (b) make the presenter
  per-connection stateful (hold `DevInfo`). (a) keeps the seam testable — prefer it. This
  **changes the `Presenter` interface**, so update `fakePresenter` in `session_test.go`.

**Design considerations:**
- The cursor rule (`field.Row, field.Col+1`) and the layout convention (title row 0, error
  just above help, help on last row) are unchanged — only the *last-row number* becomes dynamic.
- Decide a sane fallback when dimensions are unknown/زero → default to 24×80 (MOD 2).
- The bridge already echoes the negotiated terminal type to the backend, so the *bridged*
  session geometry is handled; this item is only about the proxy's **own** screens.
- MOD 5 also changes **columns** (132 wide) — decide whether to just widen/center or leave
  column layout fixed at 80 for v1 (rows are the primary pain point the user flagged).
- Tests: the screen builders become unit-testable for geometry (assert error/PF rows track the
  passed row count); the actual rendering still needs an emulator that advertises MOD 3/4.

**Suggested approach:** Parameterize the screen builders by dimensions, switch the presenter to
`HandleScreenAlt` with the negotiated `DevInfo`, and add table tests over a couple of model
sizes asserting bottom-anchored rows are computed correctly.

**Dependencies:** self-contained (touches screens + presenter only); can be done any time, but
slotted after audit since it's UX polish rather than edge-hardening.

---

## Quick reference: what's already wired for the future

| Future need | Existing hook |
|---|---|
| Backend TLS | ✅ done — per-service `tls_verify`; `bridge.Bridge` takes a `*tls.Config` |
| Configurable escape key | `Session.EscapeAID` / `bridge.EscapeAIDPA3` (hard-coded to PA3 at wiring) |
| Alternate transport (TLS in) | ✅ done — `internal/listen.Build` + `server.ServeAll`; `Server` stayed `net.Listener`-based |
| Admin via 3270 | ✅ done — `A` menu entry + adminFlow; see `internal/server/admin*.go` |
| Audit | `Session.Run` sees Identity + service + bridge Cause; add an `Auditor` seam |
| Pluggable identity source | `auth.UserStore` interface already abstracts the store (LDAP later = new impl) |
| Larger terminals (MOD 3/4/5) | `go3270` `DevInfo.AltDimensions()` + `HandleScreenAlt`; screen builders need to take dimensions |
