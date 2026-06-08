# Admin Lock on Self-Service User Settings (`user_settings_locked`) — Design

**GitHub issue:** #86
**Related:** #63 (self-service User Settings — this is its explicit "Out of scope" follow-up),
#47 (TOTP MFA), #46 (admin edit-user form), #48 (unified auth throttling) — all merged.
**Date:** 2026-06-08

## Goal

Give an admin a per-user switch that **removes a user's access to the self-service User
Settings (`0`) menu** — so the user cannot change their own password or enroll / re-enroll /
disable MFA. The capability is admin-only and is the planned residual of #63 (see that
spec's "Out of scope": *admin ability to prohibit opt-in for specific accounts*).

## Motivation

Shared / guest / demo accounts must stay usable by many people. The intended use case is a
public login (e.g. `guest` / `guestpassword`) that anyone can use to reach a published 3270
service such as a BBS whose own auth is sufficient. Today any such user could:

- change the shared password from inside User Settings, locking everyone else out, or
- enroll MFA and walk away holding the only TOTP authenticator.

A locked account closes both: self-service is gone, and all of its credential state becomes
admin-managed.

## Mechanism: a per-user flag, not a reserved group

Modeled as a per-user boolean `user_settings_locked`, mirroring `mfa_required` — **not** a
reserved `ZZ`-prefixed group. Rationale: groups in this system are *additive* — membership
**grants** capability (service visibility; ZZADMIN grants admin powers). A lock is
*subtractive* — it **removes** a capability. Expressing a subtractive meaning as group
membership inverts the model and reads as a footgun later ("why does being *in* a group take
an ability away?"). A flag named for its effect, sitting next to `mfa_required` on the user
form, says exactly what it does. The name is deliberately generic (`user_settings_locked`,
not `guest`/`shared`) so it can gate future settings classes without re-coupling.

## Behavioral model

`user_settings_locked` controls exactly two things: the **visibility/reachability of the
`0` User Settings entry**, and the **enrollment branch of the login MFA gate**. It does not
disable MFA and does not change the verify path.

### Login / MFA gate

The gate stays secret-first (#63 PR1). Locked accounts **skip the enrollment branch
entirely** (forced or self-service); the **verify branch is unchanged**.

| `user_settings_locked` | `mfa_secret` | login MFA behavior |
|---|---|---|
| false | `""` | per existing gate (forced enroll iff `mfa_required`) |
| false | set | verify (secret-first) — unchanged |
| **true** | set | **verify** — prompted for the code at login, exactly like any MFA user |
| **true** | `""` | **no MFA prompt**, straight to menu; enrollment never triggered |

Consequence: on a **locked + no-secret** account, `mfa_required` is **inert** (no secret to
verify, enrollment suppressed). This is surfaced as a hint on the admin form, not enforced
as a hard guard — the combination is harmless.

### Admin-managed MFA workflow

MFA is *allowed* for locked accounts but entirely admin-driven. The provisioning path reuses
existing flows with no new admin-enrollment UI:

1. Admin creates a normal (unlocked) account and sets its password.
2. Admin logs in as that user and self-enrolls MFA via the existing `0` flow; captures the
   secret/key.
3. Admin distributes the key to the intended individuals.
4. Admin sets `user_settings_locked = true`.

From then on the account prompts for the MFA code at login (verify branch) but exposes no
self-service. To rotate the password or secret, or turn MFA off, the admin temporarily
unlocks → makes the change (via self-login as above, or the admin user form for password /
`Clear MFA`) → re-locks.

### Menu

When locked, the `0 User Settings` meta-row is **omitted** from the menu (it is hidden, not
shown-and-blocked). The menu meta-band must handle all four combinations:

| admin (ZZADMIN) | locked | meta band |
|---|---|---|
| no | no | `0` only |
| no | yes | *(empty)* |
| yes | no | `0` + `A` |
| yes | yes | `A` only |

PA3/PF3 semantics are unchanged.

## Components & changes

### 1. Store (`internal/store`)

- New column on `users`: `user_settings_locked INTEGER NOT NULL DEFAULT 0`, alongside
  `mfa_required`.
- `User` struct gains `UserSettingsLocked bool`; `scanUser` and the list query in
  `admin.go` read the column (same pattern as `mfa_required`).
- New setter `SetUserSettingsLocked(ctx, userID int64, locked bool) error`, mirroring
  `SetMFARequired` (returns `ErrNotFound` for an unknown id).

### 2. Admin UI (`internal/server/admin_users.go`, `internal/screens`)

- The user form gains a Yes/No field **"User Settings Locked"**, adjacent to "MFA Required",
  read/written through the existing `ui3270.RunForm` plumbing.
- A static hint near the field for the inert case: when locked with no secret, `mfa_required`
  has no effect (informational text, not a guard).
- **No self-lock guardrail.** Locking your own settings keeps full `A` admin powers, so
  there is no lockout risk (unlike no-self-delete / no-self-demote, which exist to prevent
  lockout). Allowed and unremarkable.

### 3. Login / menu (`internal/server/session.go`, `internal/screens/menu.go`)

- The locked flag is loaded with the user record so it is available at both gate and
  menu-render time (the gate already loads the user for MFA state).
- `Session.mfaGate`: when the user is locked, the **enrollment** branch is skipped; the
  verify branch is unchanged (see table above).
- `screens.MenuScreen` takes a new `settingsLocked bool` input; when true the `0` meta-row
  is not emitted. The meta-band layout logic (`menuBottomRow`, the `0`/`A` placement)
  handles the four combos, including the empty band.

### 4. Server-side enforcement (defense in depth)

- The menu dispatch (`menuUserSettings` case in the `Menu`-choice switch) rejects a `0`
  selection from a locked user even though the row is not rendered — the gate is enforced
  server-side, not merely by hiding the row. Mirrors the existing "non-admin can't reach the
  admin flow even if a buggy presenter says so" guard.

### 5. Seeding (`internal/seed`)

- `SeedUser` gains an optional `user_settings_locked` JSON field (default false), so guest
  accounts are provisionable declaratively. `seed` is idempotent; this mirrors the existing
  user fields. Documented in `seed.example.json` if a sample is warranted.

### 6. Audit (`internal/store`)

- Toggling the flag emits audit events — new kinds `settings_locked` / `settings_unlocked`
  (or one `settings_locked` kind with a boolean Detail), mirroring how `mfa_required` toggles
  audit as `mfa_enforced`. Recorded on the admin save path. (Actor/subject conflation of #73
  applies equally here and is **not** fixed by this issue.)

## Testing (TDD, per repo convention)

Unit tests with the fake presenter/terminal and deterministic `Session.Now`:

- **store:** column default-off; `SetUserSettingsLocked` round-trips; `ErrNotFound` for
  unknown id; `scanUser`/list surface the value.
- **screens:** `MenuScreen` omits the `0` row when locked, across all four meta-band combos
  (non-admin locked → empty band; admin locked → `A` only); present when unlocked.
- **server (gate):** locked + secret → `mfaVerify` still called; locked + no secret → no MFA
  screen, no `mfaEnroll`; unlocked behavior unchanged (regression).
- **server (dispatch):** a locked user selecting `0` is rejected (cannot reach
  `userSettings`); unlocked user still reaches it.
- **admin form:** sets and clears the flag; persists; emits the audit event.
- **seed:** `user_settings_locked` applies and is idempotent.

Then the **s3270 smoke skill** for the protocol surface: a locked user sees no `0` row, the
menu (and meta band) still renders/positions correctly, and an MFA-enrolled locked user is
prompted for the code at login. Exact emulator-pass visual polish of the empty/`A`-only meta
band is the deferred human-pass detail, not a design blocker.

## Out of scope

- Granular locks (password-only vs MFA-only) — A-or-nothing for now; the generic flag name
  leaves room to add sibling flags later (#86 discussion).
- A "shown-but-blocked" `0` entry — we hide it outright.
- Admin "enroll MFA on behalf of a user" UI — the enroll-then-lock workflow reuses existing
  self-service, no new screen.
- Fixing audit actor/subject conflation (#73) — applies here too but is a separate issue.
- The throttle mechanism itself (#48) — unchanged; no new failure paths are introduced for a
  locked account beyond the normal login/verify ones it already shares.
