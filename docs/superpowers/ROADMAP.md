# TN3270Proxy — Roadmap (remaining goals)

The MVP (connect → login → group-filtered menu → bridge → PA3-return) and the bulk of the
post-MVP hardening, admin UI, MFA, and release work are complete and on `main`. This document
captures only the **deferred follow-on milestones** with enough context to **start any of them
in a fresh session**: the goal, where it hooks into the existing code, design considerations, a
suggested approach, and dependencies.

The milestone numbers below are non-contiguous (4 and 6) because they preserve the original
spec §9 numbering — the lower-numbered milestones have shipped and were removed from this doc.
The `M.` entry is outside that scheme: it is maintenance, not a spec milestone.

**Process reminder:** each numbered milestone below is its own brainstorm → spec → plan →
implementation cycle, same as the MVP. New specs go in `docs/superpowers/specs/`, plans in
`docs/superpowers/plans/`. See `CLAUDE.md` for architecture and conventions. The `M.`
maintenance item does not need that cycle — it is a one-PR change plus verification.

**Next up: the Go 1.26 language-floor bump (below), which is time-boxed to the Go 1.27
release window.** Then #4 (TN3270E support), then #6 (scale), which is deferred until real
load data justifies it.

---

## M. Maintenance: Go 1.26 language floor

Not a spec §9 milestone — a time-boxed maintenance item, listed here because it is the only
scheduled work with an external deadline.

**Goal:** Raise the `go.mod` language floor from `go 1.25.0` to `go 1.26.0`. The *build
toolchain* already moved to `go1.26.6` (PR #152, shipped in v0.8.15); this is the separate
second half.

**When:** Go 1.27 was at rc3 on 2026-08-19 and ships in August 2026. Go patches a major
release only until two more ship, so Go 1.25 goes end-of-life the day 1.27 lands. Do this
within a release cycle of that date. The toolchain bump already removed the urgency — the
govulncheck gate is safe on go1.26.x — so this is currency, not a fire.

**Where it hooks in:**
- `go.mod` — the `go 1.25.0` line. That is the whole code change.
- `docs/install/03-from-source.md` — states "Go 1.25 or newer"; must move with it.
- `docs/dev/dependencies.md` — the **Toolchain currency policy** section holds the checklist
  and the reasoning. Update the dated log entry there.

**Design considerations:**

The `go` line is not cosmetic. It selects GODEBUG defaults, so it can change runtime
behavior. The Go 1.26 defaults were checked against this codebase:

| GODEBUG | Effect here |
|---|---|
| `tlssecpmlkem` | **Real change.** Enables the `SecP256r1MLKEM768` and `SecP384r1MLKEM1024` post-quantum key exchanges by default. No code sets `Config.CurvePreferences`, so both TLS legs use the Go default group list — `internal/listen` (inbound) and `internal/bridge` (backend dial). |
| `cryptocustomrand` | None. Every call site already passes `crypto/rand.Reader`. |
| `urlstrictcolons` | None. No package imports `net/url`. |

The TLS change is the one that matters for a gateway that dials legacy hosts. A rigid backend
TLS stack can reject a larger ClientHello or unknown groups. Escape hatches if it bites:
set `Config.CurvePreferences`, or ship `GODEBUG=tlssecpmlkem=0`.

Raising the floor also raises the minimum Go for source builders. `GOTOOLCHAIN=auto`
downloads it automatically, so this is a documentation concern, not a breakage.

**What it unlocks** (each needs the floor at 1.26 before the compiler will allow it):
- `slog.NewMultiHandler` — replaces the hand-rolled `multiHandler` in `internal/logging`.
- `errors.AsType` — type-safe generic `errors.As`.
- `new(expr)` — `new` accepting an initial-value expression.

None of these are the reason to do the bump; take them as follow-ups if they read better.

**Suggested approach:** One PR, one line in `go.mod` plus the two doc updates. Then run the
`s3270-smoke-testing` skill, and — because unit tests will not catch a handshake regression —
verify a real TLS backend still bridges. Confirm the inbound leg against a live emulator too.

**Dependencies:** Go 1.27 shipping (or close enough to it). Nothing in the codebase blocks it.

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
| Configurable escape key | `Session.EscapeAID` / `bridge.EscapeAIDPA3` (hard-coded to PA3 at wiring) |
| Pluggable identity source | `auth.UserStore` interface already abstracts the store (LDAP later = new impl) |
| Multiple endpoints per service (#6) | `store.Service` is single host:port today; selection policy would live in the session |
