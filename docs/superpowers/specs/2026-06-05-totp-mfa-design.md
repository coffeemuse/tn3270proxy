# Design: optional TOTP MFA — admin-enforced, self-enroll, encrypted-at-rest

**Issue:** [#47](https://github.com/coffeemuse/tn3270proxy/issues/47)
**Date:** 2026-06-05
**Status:** Approved design, pre-implementation
**Depends on:** #46 (Edit User Details form — merged, PR #58) · **Known gap deferred to:** #48 (auth throttling)

## Problem

The gateway authenticates with a password only. On an eventually-public TN3270 server, a
single factor is weak. We want an **optional, admin-enforced** TOTP (RFC 6238) second factor:
the admin turns it on per user; the user self-enrolls on next login (the admin never sees the
secret); subsequent logins prompt for a 6-digit code between password-success and the menu.

A TOTP shared secret is **reversible** — it must be read back to compute codes — so it cannot
be hashed like the password. It must be **encrypted at rest** so a stolen `proxy.db` alone is
useless. The encryption key is an infra-level secret, sourced from the startup environment,
**not** the runtime DB params table.

This is the MVP control model: **admin-assigned per user**. User opt-in / hybrid policy is a
later iteration and out of scope.

## Decisions (from brainstorming)

- **Library:** `pquerna/otp` — the de-facto vetted Go TOTP/HOTP library (RFC 6238/4226). No
  hand-rolled crypto.
- **Secret size: 80-bit (16 base32 chars, 4 chunks of 4).** Manual key entry is the only
  enrollment path on a 3270 (QR scanning is impossible), so secret length is a typing-burden
  tradeoff. 80-bit is the Google Authenticator default and the lightest to hand-type; 128/160-bit
  were rejected as marginal benefit behind a password for double the typing.
- **Master key source: env var OR config file, env wins.** AES-256-GCM; key is 32 bytes,
  base64-encoded. `internal/config` gains env ingestion (new — `Load` is config-file/flags only
  today).
- **Missing key → fail closed.** At startup, if any user has a non-NULL `mfa_secret` but no key
  is configured, the proxy **refuses to start** (fatal). No key + no enrolled users = starts fine.
- **Wrong key → fail closed via a startup sentinel.** A key-check sentinel (a known value
  encrypted with the key) is stored in the DB. At startup, if a sentinel exists and the current
  key can't decrypt/authenticate it, the proxy **refuses to start** — loud and immediate, before
  any user is locked out at login. The first run with a key writes the sentinel.
- **Break-glass recovery.** A new CLI subcommand `tn3270proxy mfa reset-all` wipes every
  `mfa_secret` + `mfa_enrolled_at` and rewrites the sentinel for the current key. This is the
  lost-key / rotation escape hatch: the system is recoverable rather than permanently bricked;
  every still-required user re-enrolls on next login.
- **Secret lifecycle.** The secret is generated in **session memory** and held across enrollment
  retries (a mistyped code re-shows the *same* key). It is persisted (encrypted) **only** on a
  successful confirm. `mfa_secret` stays NULL = "enrollment pending" until that moment. The admin
  never sees a secret; admin "clear" only deletes.
- **Replay + skew.** Verification accepts the current code **±1 time step** (~90s, device clock
  skew); it rejects any code whose time-step counter is **≤ `mfa_last_step`** and advances
  `mfa_last_step` on every success (single-use within the window).
- **Issuer** is a new runtime sysconfig key `MFA_ISSUER` (default `TN3270PROXY`), admin-editable
  via the System Parameters form, validated non-empty and colon-free (otpauth labels use `:`).
- **Known gap — brute force.** v1 ships **without** per-attempt throttling; a 6-digit code is
  brute-forceable under unlimited tries. The ±1/replay logic is **not** a brute-force defense.
  Closed by #48; tracked there, not here.

## Data model (`internal/store`, via the existing `ensureColumn` migration pattern)

New columns on `users`:

| Column            | Type             | Meaning                                                        |
|-------------------|------------------|---------------------------------------------------------------|
| `mfa_required`    | INT NOT NULL d.0 | Admin enforce flag.                                            |
| `mfa_secret`      | TEXT NULL        | AES-256-GCM ciphertext (base64), NULL = not enrolled.         |
| `mfa_enrolled_at` | TEXT NULL        | UTC RFC3339 timestamp of successful enrollment.               |
| `mfa_last_step`   | INT NOT NULL d.0 | Highest accepted TOTP time-step counter (replay floor).       |

Derived **status**: `required && secret == NULL` → PENDING; `required && secret != NULL` →
ENROLLED; `!required` → NONE.

**Sentinel** lives in `system_config` under a reserved key (e.g. `MFA_KEY_CHECK`) — a fixed
plaintext encrypted with the current master key. It is *not* a user-editable sysconfig `Entry`
(not in `Catalog`); it's written/read by the mfa layer directly. (If keeping it out of the
operator-facing params table is cleaner, a dedicated one-row table is an acceptable alternative;
decide at implementation time.)

New store methods (all SQL stays in `store`): set/clear MFA enforcement, store enrolled secret +
timestamp, read MFA fields for a user, update `mfa_last_step`, count enrolled users (for the
startup fail-closed check), and the break-glass reset-all.

## New package: `internal/mfa`

Pure crypto + TOTP, no DB/network (mirrors how `screens` is pure rendering, `auth` never touches
the network). Holds the master key.

- **`NewCipher(key []byte) (*Cipher, error)`** — validates a 32-byte key, builds the AES-GCM AEAD.
- **`Seal(plaintext) (string, error)` / `Open(ciphertext string) ([]byte, error)`** — base64
  AES-256-GCM with a random nonce prepended; `Open` returns a typed error on auth failure (used
  by the sentinel check and the per-login decrypt).
- **`GenerateSecret(issuer, account string) (Secret, error)`** — wraps `pquerna/otp/totp`
  Generate at 80-bit; exposes the raw base32 (for chunked display) and the otpauth provisioning
  data.
- **`Validate(secretBase32 string, code string, lastStep uint64) (ok bool, newStep uint64)`** —
  ±1 step window, replay floor at `lastStep`. Returns the accepted step so the caller persists it.
- **`Chunk(base32 string) string`** — `ABCD EFGH IJKL MNOP` grouping for the enrollment screen.

The proxy builds one `*mfa.Cipher` at startup from the resolved key and threads it into the
session via a new seam (see below). Nil cipher = MFA disabled (no key configured, no enrolled
users) — sessions skip the MFA step entirely.

## Config & startup (`internal/config`, `cmd/tn3270proxy`)

- `Load` resolves the master key: **env var** (e.g. `TN3270PROXY_MFA_KEY`) **wins over** a config
  file field (e.g. `mfa.key` or `mfa.key_file` — a path keeps the raw key off the JSON; decide at
  implementation). Value is base64 of 32 bytes; a present-but-malformed key is a fatal config
  error.
- **Startup fail-closed sequence** (in `serve` wiring, after the store opens):
  1. Count enrolled users (`mfa_secret != NULL`).
  2. If count > 0 and **no key** → fatal: refuse to start.
  3. If a key is present and a **sentinel exists** but won't decrypt → fatal: refuse to start.
  4. If a key is present and **no sentinel exists** → write the sentinel (first-run init).
  5. Build `*mfa.Cipher`; pass to the session handler.
- The key is **never logged** (CLAUDE.md hard rule; same class as passwords).

## Login flow (`internal/server/session.go`)

A new step between `doLogin` success (currently line 407, returns the identity) and
`maybeShowNews` (line 244). Enumeration safety is preserved: the password is verified **first**;
only on a correct password do we branch to MFA. A wrong password gives today's uniform failure and
never reveals MFA status.

```
doLogin OK
  └─ mfaGate(identity):
       cipher == nil OR !user.mfa_required           → proceed (no MFA)
       mfa_required && mfa_secret == NULL             → EnrollMFA screen
       mfa_required && mfa_secret != NULL             → VerifyMFA screen
  └─ maybeShowNews → menu
```

- **Enroll:** generate secret in session memory (held across retries). On a valid confirm code,
  Seal + persist secret + `mfa_enrolled_at`, set `mfa_last_step` to the accepted step, audit
  `mfa.enrolled`, proceed. Invalid code → re-show the *same* secret with an error. PF3 → cancel to
  login (nothing persisted; a fresh secret is generated next time).
- **Verify:** Open the stored secret, `mfa.Validate` with ±1 step and the replay floor. Success →
  persist the new `mfa_last_step`, audit `mfa.success`, proceed. Failure → re-prompt, audit
  `mfa.failed`. A decrypt failure here should not occur (sentinel guards it at startup); treat it
  as an infrastructure error (log + audit), not a silent re-enroll. PF3 → cancel to login.
- **Idle timeout** during either screen is classified like the existing screens: audit
  `idle timeout` / re-arm pre-auth, consistent with `doLogin`/`maybeShowNews`.

Both screens sit behind **new `Presenter` methods** so the session is unit-tested with the
existing fake terminal (no live 3270 client):

```go
// EnrollMFA shows the secret (chunked) and prompts for a confirmation code.
EnrollMFA(conn net.Conn, term Term, issuer, account, chunkedSecret, errMsg string) (code string, quit bool, err error)
// VerifyMFA prompts an enrolled user for a code.
VerifyMFA(conn net.Conn, term Term, errMsg string) (code string, quit bool, err error)
```

## Screens (`internal/screens`)

Two builders, following the screen convention (title row 0, error line just above the PF-help
line, PF help on `geom.HelpRow()`, cursor on the code field via `cursorAt`). Take a `Geometry`
first param; taller models push the help row down. Field-name constants added (e.g.
`FieldMFACode`).

**Enrollment** (MOD 2 shown):

```
 MFA ENROLLMENT - SECURITY KEY SETUP

 Multi-factor authentication is now required for your account.
 Enter the key below into your authenticator app (any TOTP app),
 then type the current 6-digit code to confirm enrollment.

     Issuer:   TN3270PROXY
     Account:  ROBERT
     Key:      ABCD EFGH IJKL MNOP

     Confirmation code:  [______]


 Code incorrect - check the key and try again
 ENTER=Confirm enrollment   PF3=Cancel (return to login)
```

**Verification:**

```
 MFA VERIFICATION

 Enter the current 6-digit code from your authenticator app.

     Code:  [______]



 Code incorrect - try again
 ENTER=Verify   PF3=Cancel (return to login)
```

The `otpauth://` URI is deliberately **not** shown — manual key entry is the only realistic 3270
path, and the URI is dead weight on a green screen.

## Admin controls (`internal/server/admin_users.go`, the #46 Edit User Details form)

Three additions to the existing user edit form:

- **`MFA required`** — Y/N enforce toggle. Flipping N→Y audits `mfa.enforced`.
- **`MFA status`** — display-only: NONE / PENDING / ENROLLED.
- **`Clear MFA`** — Y on save wipes `mfa_secret` + `mfa_enrolled_at` (lost-device recovery; the
  user re-enrolls next login if still required), audits `mfa.cleared`. Admin never sees a secret;
  clear only deletes.

Admin changes apply at the next menu render / login (live sessions are not re-evaluated — existing
convention).

## CLI (`cmd/tn3270proxy`)

New `mfa` subcommand group:

- **`tn3270proxy mfa reset-all -db proxy.db`** — break-glass: wipe all `mfa_secret` +
  `mfa_enrolled_at`, rewrite the sentinel for the current key. Requires the key to be configured
  (so the rewritten sentinel matches). Prints how many users were reset.

## Audit events (`internal/store` audit kinds, ties to #40)

`mfa.enrolled`, `mfa.success`, `mfa.failed`, `mfa.cleared`, `mfa.enforced` — session-correlated,
username-tagged, UTC RFC3339, via the existing `RecordAudit`/`Auditor` seam. Never log the secret
or the entered code.

## Testing

- **TDD throughout** (repo convention; `go test ./... -race`).
- `internal/mfa`: Seal/Open round-trip, Open auth-failure on a wrong key, Validate ±1-step accept,
  out-of-window reject, replay reject at/below `lastStep`, chunking.
- `internal/store`: column migration, set/clear/store/read, `mfa_last_step` update, enrolled-count,
  reset-all.
- `internal/screens`: field names/content present (not row numbers — positioning is verified in a
  real emulator per convention).
- `internal/server`: session MFA gate with a fake Presenter — pending→enroll, enrolled→verify,
  wrong-code retry retains the secret, PF3 cancels to login, idle classification, enumeration
  safety (no branch before password success). Startup fail-closed: enrolled-but-no-key, wrong-key
  sentinel.
- `cmd/tn3270proxy`: `mfa reset-all` clears rows and rewrites the sentinel.
- **Protocol surface** (cursor on the code field, screen content, PF3/ENTER, the enroll→verify
  flow) verified with the **s3270-smoke-testing skill** before declaring done; a human pass in
  c3270 is the final word on visual polish.

## Out of scope

- Per-attempt throttling / lockout (#48).
- User-managed MFA policy / opt-in / hybrid (later iteration).
- QR display (impossible on 3270).
- Key rotation tooling beyond `reset-all` (re-encrypt-in-place under a new key is a future nicety;
  reset-all + re-enroll is the v1 recovery path).
