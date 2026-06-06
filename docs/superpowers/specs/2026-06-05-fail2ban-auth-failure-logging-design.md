# fail2ban-friendly auth-failure logging — design

**Issue:** GH #51
**Status:** design
**Date:** 2026-06-05

## Goal

Emit a **stable, parseable log line on every authentication failure** so an
operator can wire an external log-scanner (fail2ban being the canonical example)
to ban abusive untrusted sources. We provide the *hook* — a documented,
drift-resistant log surface plus a sample filter — not the trap: installing and
configuring fail2ban is the admin's job and out of scope here.

The durable SQLite audit trail (queried via `audit list`, and the planned admin
viewer #40) is the forensic record. This issue adds the **real-time, tail-able**
surface that the audit trail is not.

## What already exists (and reshapes this issue)

Every prerequisite the original issue named has landed since it was filed:

- **Structured logging (slog).** `internal/logging.New` fans every record out to
  **two concurrent sinks** via `multiHandler`: human-readable **text to stderr,
  always** (the friendlier console/journald stream), and — when `log.file` /
  `-log-file` is set — machine-readable **JSON to that file, additionally** (the
  parse-for-tooling stream). Both honour the same level and carry the same fields;
  they are in use at the same time, not an either/or choice. The same auth-failure
  line therefore appears in both formats, and an operator may point a scanner at
  whichever sink suits their pipeline. The current password-fail site already
  emits `s.log().Warn("auth failed", "user", user)` on a connection logger
  pre-tagged with `remote=IP:PORT`.
- **Trust signal (#43).** `Session.Trusted` is the authoritative trust decision —
  the DB-backed trusted-network list (admin UI → Trusted Networks; the old
  `limits.trusted_cidrs` config key was removed in favour of it), set in
  `sessionFor` via `limits.Trust.IsTrusted(addr)`. Available directly at the
  failure sites.
- **Auth throttling (#48).** Shipped as per-username **linear backoff**
  (delay-then-re-prompt), **not** attempt-cap-then-disconnect.

The consequence of the #48 shape is central to this design: **there is no
"max attempts reached, disconnecting" event.** Backoff never disconnects. So the
old proxy's *second* log line has no corresponding event, and the per-failure
line is the only ban signal — which makes this log surface the **actual
perimeter-ban mechanism** (fail2ban counts the repeated lines via its own
`maxretry`/`findtime`).

## The stable log line (the contract)

A single helper, `(*Session).logAuthFailure(username, reason string)`, emits the
documented line at every auth-failure site. On the JSON file sink it renders as:

```json
{"time":"2026-06-05T19:00:54Z","level":"WARN","msg":"auth failed",
 "remote":"203.0.113.7:51324","user":"alice","src":"203.0.113.7",
 "trusted":false,"reason":"invalid_credentials"}
```

and on stderr text as:

```
level=WARN msg="auth failed" remote=203.0.113.7:51324 user=alice src=203.0.113.7 trusted=false reason=invalid_credentials
```

Fields:

| Field | Source | Purpose |
|---|---|---|
| `msg="auth failed"` | literal | Stable anchor (already the contract in `session_logging_test.go`). |
| `remote` | connection logger (`IP:PORT`) | Ops correlation; **not** the fail2ban host (port makes `<HOST>` fragile). |
| `user` | attempted username | Forensics; emitted exactly once (see below). Never the password. |
| `src` | IP-only, `net.SplitHostPort(remote)` | The fail2ban `<HOST>` binding — bare IP, no port. |
| `trusted` | `Session.Trusted` | Lets the filter ban `trusted=false` only. |
| `reason` | call site, **coarse** | `invalid_credentials` (password) or `bad_mfa` (TOTP). |

**Coarse reason, by decision.** `auth.Authenticate` keeps returning a uniform
`ErrInvalidCredentials`; we do **not** distinguish unknown-user vs bad-password
even in the log. Zero enumeration surface, and no change to the `auth` package.
fail2ban needs only host + untrusted marker to count — not the reason.

**`src` plumbing.** Parse the host once in `sessionFor` (from the connection's
`RemoteAddr`) and hold it on the `Session` (e.g. `RemoteHost string`). The helper
reads `s.RemoteHost`. `remote=IP:PORT` stays on the logger unchanged.

**No duplicate `user` attr.** At the password-fail site the connection logger is
pre-auth (carries `remote`, no `user`), so the helper supplies `user`. At the MFA
sites the logger has already been enriched with `user`. The helper must emit the
attempted username exactly once regardless — settle the mechanism in the plan
(e.g. the helper always emits on the base/remote-tagged logger and supplies
`user` itself, rather than relying on prior enrichment).

## Emit sites

1. **Password fail** — `internal/server/session.go`, `doLogin`: replace the
   existing `s.log().Warn("auth failed", "user", user)` with
   `s.logAuthFailure(user, "invalid_credentials")`.
2. **MFA verify fail** — `mfaVerify` `!ok` branch: add
   `s.logAuthFailure(u.Username, "bad_mfa")`. (No log line exists here today —
   only an `AuditMFAFailed` record.)
3. **MFA enroll-confirm fail** — `mfaEnroll` `!ok` branch: add
   `s.logAuthFailure(u.Username, "bad_mfa")`. (Same — audit-only today.)

The audit records and #48 backoff at these sites are unchanged; the helper only
adds the stable log line beside them. Credential content (password, TOTP code,
MFA secret) is never logged — existing CLAUDE.md hard rule.

## Dropped: the second "max attempts reached" line

The original issue (mirroring the old proxy) asked for a second stable line for
an attempt-cap disconnect. **Dropped** — #48 is backoff, not lockout, so no such
event exists. Documented behaviour instead: fail2ban's own `maxretry`/`findtime`
counts the repeated per-failure lines; that is the ban trigger. This is a
conscious deviation from the issue text, recorded here.

## Documentation (the operator contract)

A docs section (README front door now; folded into the #66 admin guide later):

- Note both sinks run concurrently and carry the same line; the operator picks
  whichever their scanner tails. **Recommend the JSON file sink**
  (`-log-file /var/log/tn3270proxy/auth.json` or `log.file` in config) as the
  fail2ban target where possible — structured and unambiguous — while documenting
  the stderr-text form as an equally valid alternative for stderr/journald
  pipelines.
- A **sample** fail2ban filter + jail for each sink, explicitly "example, adapt
  to your deployment." JSON form, e.g.:
  ```
  [Definition]
  failregex = "msg":"auth failed".*"src":"<HOST>".*"trusted":false
  ```
  and the equivalent stderr-text form
  (`msg="auth failed" .*\bsrc=<HOST>\b.*\btrusted=false\b`).
- A "**stable contract — changing the field set breaks operator filters**"
  comment at the helper and in the docs.
- Untrusted-only guidance: match `trusted=false` so trusted sources are never
  banned (they are still logged, with `trusted=true`, for ops visibility).

## Testing (TDD)

Extend `internal/server/session_logging_test.go` (the JSON `bufLogger` harness is
already there):

- Password fail line carries `src` (bare IP), `trusted` (`false` for an
  untrusted session, `true` for a trusted one), and `reason=invalid_credentials`.
- New MFA-fail test: `reason=bad_mfa` at the verify (and enroll-confirm) site.
- Re-assert the submitted password and the entered TOTP code never appear in log
  output.
- A **shape-pinning** test over the emitted field set, so the contract can't
  silently drift (the issue's "test asserts the log-line shape" AC).

`go test ./... -race`.

## Out of scope

- The attempt-cap/disconnect line (no such event under #48 backoff).
- Any change to `auth.Authenticate` (coarse reason needs none).
- Shipping/installing/configuring fail2ban itself, or any new log sink — we reuse
  the existing stderr-text + optional-JSON-file logging; no new config surface.
- The admin Audit viewer (#40) — complementary forensic view, separate issue.

## Acceptance criteria

- [ ] Every auth failure (password, MFA verify, MFA enroll-confirm) emits the
      stable line carrying `src` (`<HOST>`) + `trusted` marker + coarse `reason`.
- [ ] On-screen failure messages remain uniform (no username enumeration);
      `auth.Authenticate` unchanged.
- [ ] Credential content never logged (password / TOTP code / MFA secret).
- [ ] README/docs include a sample fail2ban filter + jail (JSON-file primary,
      text alternative) that bans untrusted sources only, marked as an example.
- [ ] A test pins the field set so the shape doesn't drift.
- [ ] `go test ./... -race` green.

## Relationship to other issues

- **#48** (throttling) — done; its backoff-not-lockout shape is why the second
  line is dropped and why this surface is the real ban mechanism.
- **#43** (DB-managed trusted-network list) — source of the `trusted` marker via
  `Session.Trusted`.
- **#40** (admin Audit viewer) — forensic counterpart; this is the real-time ban
  surface.
- **#66** (release docs) — the sample filter/jail folds into the admin guide.
