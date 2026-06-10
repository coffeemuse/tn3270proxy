# MFA administration

The gateway supports TOTP (authenticator-app) MFA. Codes are entered at
login after the password; secrets are stored encrypted under the
[MFA master key](../install/06-mfa-key.md), which is infrastructure-level
configuration — if no key is configured, MFA is disabled entirely.

## The enforcement model: secret-first, opt-in friendly

Two independent per-user facts drive behavior:

1. **MFA required** (the admin flag on the [user form](03-users.md)), and
2. **an enrolled secret** (created when the user completes enrollment).

The rules:

- **Any stored secret is verified at login** — regardless of the required
  flag. A user who opted in through User Settings is challenged every login
  even though you never required it. Setting required = N for an enrolled
  user does **not** stop the challenges; only clearing the secret does.
- **Required = Y with no secret** forces enrollment at the user's next
  login: they're shown a setup key to enter into their authenticator app and
  must confirm with a valid code before reaching the menu.
- **Users can self-enroll, re-enroll, or disable** their own MFA from the
  `0 User Settings` menu (each action gated by a password re-entry).
  Self-disable is only allowed when the admin hasn't set required = Y.

⚠️ Display caveat: the user form's **MFA status** field shows `NONE` whenever
required = N — including for self-enrolled users who do have an active
secret. Don't conclude from `NONE` that no MFA is in play.

## Common operations

- **Require MFA for a user:** edit the user, MFA req = `Y`. Status shows
  `PENDING` until they enroll at next login.
- **User lost their authenticator:** edit the user, Clear MFA = `Y`. If
  required, they re-enroll at next login; if they were opt-in, they can
  re-enroll via User Settings. Audited as `mfa_cleared`.
- **Shared/guest accounts:** set the **User Settings lock** so no individual
  holder can enroll a TOTP secret and lock the others out — see
  [Users](03-users.md).
- **Wipe every enrollment** (lost master key): `tn3270proxy mfa reset-all` —
  see the [CLI reference](14-cli.md) and the
  [master key page](../install/06-mfa-key.md).

## The issuer label

The **MFA Issuer** [system parameter](07-system-parameters.md) (default
`TN3270PROXY`) is the account label users see in their authenticator app.
Set it before anyone enrolls; changing it later affects only new
enrollments.

## Brute-force protection

Failed MFA codes share the same per-username backoff as failed passwords —
see [Auth throttling in System Parameters](07-system-parameters.md). Failures
are audited as `mfa_failed` with the applied delay in the detail.
