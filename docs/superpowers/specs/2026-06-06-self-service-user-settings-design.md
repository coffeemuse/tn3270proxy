# Self-Service User Settings (menu option `0`) — Design

**GitHub issue:** #63
**Related:** #46 (admin edit-user form), #47 (TOTP MFA), #48 (unified auth throttling) — all merged.
**Spun-off follow-up:** #73 (audit trail conflates actor and subject).
**Date:** 2026-06-06

## Goal

Give every authenticated user a self-scoped settings screen reached from the service
menu via a new option `0 — User Settings`. It is a slimmed-down, self-only mirror of the
ZZADMIN user form: a user manages **their own** account only — no access to other users,
groups, or services. Scope: **change my password** and **manage my own MFA** (enroll /
re-enroll / disable).

This complements #47: today a user only gets MFA reactively (an admin sets `mfa_required`
and the next login force-enrolls them). With User Settings a user can proactively harden
their own account — opt into MFA with no admin involvement, re-enroll after a lost
authenticator without an admin `Clear MFA`, and rotate their password themselves.

## Core principle: `mfa_required` means *mandatory*, absence does **not** prohibit

`mfa_required` is an admin's *enforcement* flag. Its absence must not prevent a user from
voluntarily enrolling. The state space is fully expressed by the existing
`(mfa_required, mfa_secret)` columns — **no schema change**:

| `mfa_required` | `mfa_secret` | meaning | self-service actions |
|---|---|---|---|
| false | `""` | no MFA (default) | **Enroll (opt-in)** |
| false | set | opt-in enrolled | Re-enroll, **Disable** |
| true | `""` | pending forced enrollment (login gate force-enrolls before the menu) | none reachable from menu |
| true | set | required + enrolled | Re-enroll only — **cannot self-disable** |

## Decomposition — two PRs

- **PR1 — secret-first MFA gate** (no UI). Reorder `Session.mfaGate`; add regression
  tests; ship. Independently correct and small.
- **PR2 — self-service User Settings** (everything else). Builds on PR1.

Keeping the enforcement-core change in its own PR makes it reviewable on its own and means
the UI work never reasons about a half-changed gate.

## PR1 — `mfaGate` reorder (enforcement core)

Today `Session.mfaGate` (`internal/server/session.go`) returns early on `!u.MFARequired`,
so a stored secret is only verified for admin-required users. An opt-in secret
(`mfa_required=false`, secret set) would therefore be **decorative** — never checked.
The gate becomes **secret-first**:

```go
if s.MFA == nil      { return true, "", nil }   // MFA disabled globally
u := GetUserByUsername(...)
if u.MFASecret != "" { return s.mfaVerify(...) } // enrolled by ANY path → always verify
if u.MFARequired     { return s.mfaEnroll(...) }  // required, not yet enrolled → force
return true, "", nil                              // neither → no MFA, pass
```

**Intended side effect / regression to cover:** with secret-first, demoting an already-
enrolled user to `mfa_required=false` (admin un-checking the box) **keeps verifying them**
until the secret is cleared, instead of today's silent bypass. This is the more correct
behavior.

**Tests** (fake presenter, deterministic `Session.Now`):
1. secret + `required=false` → `mfaVerify` is called *(the opt-in case; today's bug)*.
2. secret + `required=true` → `mfaVerify`.
3. no secret + `required=true` → `mfaEnroll`.
4. no secret + `required=false` → pass (no MFA screen).
5. `s.MFA == nil` → pass.

**Docs:** add a one-line invariant to CLAUDE.md's MFA section — *login enforcement is
secret-first, not `mfa_required`-gated.*

## PR2 — menu entry + `menuChoice` refactor

- `classifyMenuSubmit` (`internal/server/presenter.go`) already returns a `menuChoice`;
  add a `userSettings` value and make it recognize `"0"`. `0` already passes the
  `FieldSelection` `NumericOnly` constraint (service rows number from `1`), so no
  field-rule change.
- Replace the `Menu` presenter return
  `(*store.Service, adminSel bool, quit bool, err error)` with
  `(*store.Service, menuChoice, error)` — collapses the two-bool encoding of a
  mutually-exclusive choice. Consumers to update: the real `go3270Presenter.Menu`
  (`presenter.go`), the `fakePresenter` in `session_test.go`, and the `session.go`
  dispatch switch (`menuService` → bridge, `menuAdmin` → `adminFlow` *iff* admin,
  `menuUserSettings` → `userSettingsFlow`, `menuQuit` → logoff). The existing
  "non-admin can't reach admin flow even if a buggy presenter says so" guard is preserved.
- `screens.MenuScreen` renders a `0  User Settings` meta-row alongside `A`, consuming one
  menu-capacity row. Available to **every** authenticated user (unlike `A`, ZZADMIN-only).
- PF3 from User Settings returns to the service menu (uniform PF3-steps-back). PA3 stays
  inert on proxy-owned screens.

## `userSettingsFlow` (new seam)

New file `internal/server/user_settings.go`, mirroring `adminFlow` but holding
`identity auth.Identity` directly (no list/selection of *which* user — it is always the
session's own identity). Behind a new `UserSettings` presenter method plus the existing
renderer seam, so it is unit-testable with the fake terminal. The only authorization check
is "the session's own identity"; group filtering / admin gating do not apply.

It renders an **adaptive menu** computed from
`(s.MFA != nil, user.MFARequired, user.MFASecret != "")`:

| state | rows |
|---|---|
| MFA unconfigured (`s.MFA == nil`) | `1 Change Password` |
| configured, no secret | `1 Change Password` · `2 Enroll in MFA` |
| secret, required | `1 Change Password` · `2 Re-enroll MFA` |
| secret, not required | `1 Change Password` · `2 Re-enroll MFA` · `3 Disable MFA` |

Flat adaptive rows (not a nested MFA sub-menu) keep 3270 navigation shallow: one PF3 back
to the service menu. Each row still launches its own flow/form.

## Change Password (self)

Reuse `ui3270.RunForm` + `passwordFromForm` (#46). Fields: **current / new / retype**, all
non-display. `Submit` callback:
1. `auth.Authenticate(self, current)` — re-verify the current password (bcrypt). Failure →
   throttled error, no change. This is the key difference from the admin form (#46), which
   does not ask for the current password because an admin acts on someone else.
2. `passwordFromForm` semantics: non-blank, new == retype, length.
3. **New ≠ current** — else error.
4. `auth.HashPassword` + `store.SetPassword`; `Throttle.Reset(username)`; audit
   `password_self`.

## MFA self-service

Factor the confirm-loop of `mfaEnroll` so it is callable from both the login-time gate and
User Settings (both validate the new secret with `lastStep = 0`). All three operations are
gated by a **current-password step-up** (re-prompt the current password before acting):

- **Enroll / Re-enroll:** generate a fresh secret → `EnrollMFAScreen` / `VerifyMFAScreen`
  confirm → `StoreMFAEnrollment` (replaces any existing secret). Audit `mfa_enrolled`
  (existing kind; its doc string already reads "user completed self-enrollment").
- **Disable** — available **only when `!mfa_required`**: `store.ClearMFA`. Audit
  `mfa_cleared` with `Detail="self-service"` (the admin clear path leaves `Detail` empty,
  so the two are distinguishable without a new kind). A user can never self-disable when an
  admin requires MFA.

**Why current password, not a current TOTP code, as the step-up:** a valid code would
defeat the lost-device recovery case (the lost device generates the code). The password
re-prompt also defends a hijacked already-authenticated session.

**"Complete a pending enrollment" is unreachable** and not handled here: the login-time
`mfaGate` force-marches a `(required, "")` user through `mfaEnroll` *before* the service
menu (PF3 there returns to login), so a user can never reach option `0` in a pending state.

## Throttling & audit

- Self password-change and MFA-confirm failures are auth attempts: they call the existing
  unified throttle (#48) via `failDelay` / `sleepFor`, with `Throttle.Reset` on success —
  keyed on the normalized username, shared across login / MFA / self-service. **No separate
  throttle.**
- Audit kinds (Option B — one new constant total):
  - `password_self` — **new** kind for a self password change (keeps the generic `admin`
    kind meaning "an admin did something", which the admin audit-list filter relies on).
  - `mfa_enrolled` — **reused** for opt-in enroll and re-enroll.
  - `mfa_cleared` + `Detail="self-service"` — **reused** for self-disable.
- A user can never *weaken* an admin policy from self-service (disable gated on
  `!mfa_required`).
- Never display or log the existing password, the new password, the MFA secret, or any
  entered code (existing CLAUDE.md rule).

## Testing

Unit tests with the fake presenter/terminal:
- Menu `0` dispatches to `userSettingsFlow`; `classifyMenuSubmit("0")` → `userSettings`.
- Adaptive-row visibility for each of the four states above.
- Change password: current-password re-verify required; new ≠ current enforced; success
  hashes + persists + resets throttle + audits `password_self`.
- Each MFA op behind the current-password step-up; disable blocked when `required`;
  enroll/re-enroll persist an encrypted secret and audit `mfa_enrolled`; disable clears and
  audits `mfa_cleared`+`Detail="self-service"`.
- Throttle hook fires on failure and resets on success across all self-service paths.

Then the **s3270 smoke skill** for the protocol surface: the `0` meta-row placement, cursor
position per screen, PF3 stepping User Settings → service menu. Exact screen layout (the
`0`-row placement and User Settings panel geometry) is the deferred emulator-pass detail,
not a design blocker.

## Out of scope

- Editing email / full name from self-service (possible later addition).
- **Admin ability to *prohibit* opt-in for specific accounts** (shared/guest account: if a
  shared login opts into MFA, one person's authenticator locks everyone else out). This is
  the residual of #47's "policy model" and is a separate follow-up — it must not block
  universal opt-in here.
- Fixing the audit actor/subject conflation (#73). #63's self-service rows sidestep it
  (actor == subject for self-service), so #63 is not blocked.
- The throttle mechanism itself (owned by #48) — this design only hooks into it.
