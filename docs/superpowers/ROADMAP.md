# TN3270Proxy — Roadmap (remaining goals)

The MVP (connect → login → group-filtered menu → bridge → PA3-return) and the bulk of the
post-MVP hardening, admin UI, MFA, and release work are complete and on `main`. This document
captures only the **deferred follow-on milestones** with enough context to **start any of them
in a fresh session**: the goal, where it hooks into the existing code, design considerations, a
suggested approach, and dependencies.

The milestone numbers below are non-contiguous (4 and 6) because they preserve the original
spec §9 numbering — the lower-numbered milestones have shipped and were removed from this doc.

**Process reminder:** each milestone below is its own brainstorm → spec → plan →
implementation cycle, same as the MVP. New specs go in `docs/superpowers/specs/`, plans in
`docs/superpowers/plans/`. See `CLAUDE.md` for architecture and conventions.

**Next up: #4 (TN3270E support)** — then #6 (scale), which is deferred until real load data
justifies it.

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
