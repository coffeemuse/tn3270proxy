# Unified failed-auth throttling (password + MFA) — design

**Issue:** GH #48 — *Unified failed-auth throttling (password + MFA attempts) — closes the MFA brute-force gap*
**Date:** 2026-06-05
**Status:** Approved design, ready for implementation plan
**Sequenced after:** GH #47 (TOTP MFA) — done and on `main`

## Problem

The MFA work (#47) deliberately left brute-force defense open: `mfaVerify` loops
indefinitely with no attempt cap, and a 6-digit TOTP code is 1-in-a-million —
trivially brute-forceable under unlimited tries. The password login loop
(`doLogin`) has the same unbounded shape. On a public-facing gateway both need
attempt throttling, and because throttling is one auth-layer concern it should be
done **once** for password + MFA rather than as two half-measures.

## Threat model and the key decision

The sharp, motivating threat is **single-account brute-force** — an attacker
hammering one username's password or (worse) its TOTP code. That observation
drives the whole design: the counter is keyed on the **username**, not the
connection and not the source IP.

Alternatives considered and rejected:

- **Per-session / per-connection counter** — resets on reconnect. An attacker
  scripts *connect → try once → disconnect → repeat*; every fresh connection
  starts at zero, so the delay is always zero. Provides essentially no
  protection against the actual threat. **Rejected.**
- **Per-IP as the primary key** — breaks on the browser-based TN3270 web client:
  every web user re-originates from the web gateway's single IP, so per-IP
  throttling collaterally backs off (or, with a disconnect rule, drops) all web
  users for one bad actor. Per-IP spray defense is still worth having, but as a
  *secondary* layer with an aggregator exemption — deferred to a follow-up (see
  Out of scope). **Rejected as the v1 primary.**
- **DB-persisted counters + hard lockout + admin-unlock UI** — new store and
  admin-screen surface, and hard lockout reintroduces the account-lockout DoS
  (an attacker locks a victim out by failing their username). Backoff-only needs
  none of it. **Rejected for v1.**

Per-username backoff is immune to the shared-IP problem (it does not care which
IP the attempts arrive from) and handles the headline TOTP case on the web path
and the direct path identically.

## Design overview

In-memory, per-username **linear backoff** shared across the password and MFA
failure paths, driven by three live-tunable system parameters.

After each failed auth attempt (wrong password **or** wrong MFA code), the
session delays before re-presenting the next screen:

```
delay = AUTH_DELAY_BASE_SECS * min(failcount, AUTH_MAX_TRIES)
```

`failcount` is the running count of failures for that username within the decay
window. The count resets to zero on any successful auth, and decays on its own
after `AUTH_FAIL_WINDOW_MINS` of no failures.

### Component: `authThrottle` (`internal/server/auththrottle.go`)

A `Server`-level, mutex-guarded counter map, shared across listeners in the same
way `connLimiter` is.

- **Key:** the *attempted* username, normalized `strings.ToUpper(strings.TrimSpace(user))`.
  Tracking the attempted string (even for a username that does not exist) keeps
  the path enumeration-safe — a bogus username and a real one back off
  identically — and matches the store's canonical-uppercase convention so case
  variants cannot bypass the counter.
- **State per key:** `{ count int; last time.Time }`.
- **`Fail(key string, now time.Time, window time.Duration) int`** — if
  `now.Sub(last) > window`, reset `count` to 0 first (decay); then `count++`,
  set `last = now`, return the new count.
- **`Reset(key string)`** — delete the entry. Called on any successful auth
  (password or MFA) so a legitimate user who fumbles and then succeeds carries no
  penalty forward.
- **Pruning:** a lightweight background sweeper owned by the `Server` periodically
  deletes entries whose `last` is older than the window, so a random-username
  spray cannot grow the map unbounded.
- **Time is injected** (`now` is a parameter) and reuses the existing
  `Session.Now` seam (`s.now()`), so unit tests are deterministic and never touch
  the wall clock.

### Parameters (`internal/sysconfig.Catalog`)

Added as three new catalog entries, edited on the existing System Parameters
admin screen and persisted in the `system_config` table. Read live at runtime via
`store.GetConfig(ctx, key)` — mirroring how `MFA_ISSUER` / `MOTD_FILE` are read —
so admin edits take effect without a restart. Each gets a named `Key*` constant
like the existing entries.

| Key | Label | Default | Validation |
|---|---|---|---|
| `AUTH_DELAY_BASE_SECS` | `Auth Delay Base (sec):` | `2` | integer ≥ 0; **`0` disables throttling entirely** (the off-switch) |
| `AUTH_MAX_TRIES` | `Max Auth Tries:` | `5` | integer ≥ 0; caps the delay multiplier |
| `AUTH_FAIL_WINDOW_MINS` | `Auth Fail Window (min):` | `15` | integer ≥ 1 |

Validators follow the existing `Catalog` convention (return an uppercase error
message string, or `""` when valid).

With defaults the worst-case single delay is `2 × 5 = 10s` — deliberately far
below the pre-auth idle window (default 2m), so a legitimate user's own backoff
can never trip the idle/disconnect timers. The absolute pre-auth ceiling
(`PreAuthMax`, default 5m) still applies during the sleep: a delay long enough to
exceed it disconnects the attacker, which is acceptable. Operators who set
extreme values own that interaction.

## Integration

Two call sites, one shared counter, so password and MFA attempts share a single
per-username failure count (the unified path #48 asks for).

- **`doLogin`** (`internal/server/session.go`) — on `auth.ErrInvalidCredentials`,
  after the existing `auth_fail` audit: `count := throttle.Fail(normUser, s.now(), window)`,
  compute the delay, sleep, then loop to re-present the login screen with the
  unchanged generic message. On success, `throttle.Reset(normUser)` before
  returning. Infrastructure errors (`auth_error`, e.g. a transient DB failure) do
  **not** count as failures and incur no delay — they are not auth attempts.
- **`mfaVerify`** (`internal/server/session.go`) — on a wrong code (`!ok`, after
  the `mfa_failed` audit): the same `Fail`/sleep against the **same username
  key**, so a correct password followed by wrong codes keeps incrementing one
  counter. On a correct code, `throttle.Reset`.
- **`mfaEnroll`** confirm-code loop — same treatment for consistency.

### Seams for testability

- **`Session.sleep func(time.Duration)`** — defaults to `time.Sleep`, nil-safe.
  Unit tests inject a recorder and assert the *computed delay* rather than
  actually sleeping; tests stay fast and deterministic.
- Config values are read through `store.GetConfig`, so tests set them directly in
  the store. A small `Session` helper reads + parses each param and falls back to
  the catalog default on a parse/`ErrNotFound` error (mirroring `mfaIssuer`).

### Enumeration safety (invariant to protect in review)

The delay is a pure function of `count`, identical for unknown-user /
wrong-password / wrong-MFA, and the on-screen message never changes. Nothing
about the throttle reveals whether a username exists or whether it has MFA. This
preserves the uniform-error property established by `auth.Authenticate` and the
#47 MFA gate.

### Audit

No new audit kind. Every failure is already audited (`auth_fail` /
`mfa_failed`); the applied delay and running count are added to that event's
`Detail` (e.g. `delay=6s count=3`) so operators can see active brute-force in
`audit list` without a second event type. The password and the entered code are
never logged (CLAUDE.md hard rule).

## Testing (TDD)

- **`auththrottle_test.go`** — pure unit tests on the component: increment,
  multiplier cap, decay-after-window resets the count, `Reset` clears, key
  normalization (case/whitespace), and concurrent access under `-race`.
- **`session` fake-driven tests** — a failed password produces the asserted delay
  via the sleep seam; a failed MFA code increments the *same* key as the
  password; success resets; `AUTH_DELAY_BASE_SECS=0` disables; an unknown
  username and a known username yield identical delays (the enumeration guard).
- No protocol-surface change: the defaults add only a server-side sleep, with no
  change to any screen, cursor position, or field. A manual s3270/emulator pass
  is therefore not required for this work (that skill covers screen/cursor/
  negotiation changes, of which there are none here).

## Out of scope (explicit follow-up issues)

- **Per-IP spray defense** (many usernames from one source) as a secondary layer,
  with an **aggregator/shared-source exemption** so the web gateway's shared IP is
  not penalized as a unit — distinct from today's `trusted_cidrs` (which means
  "fully trusted, skip DoS controls").
- **Real-client-IP recovery** from the web gateway (e.g. PROXY protocol on a
  dedicated listener) — the clean long-term fix that makes per-IP accurate again
  and removes the need for an aggregator exemption.
- **Hard lockout**, admin-unlock UI, and DB-persisted counters — only if a
  concrete threat model later demands them.
