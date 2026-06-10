# Managing users

Admin menu option **1**. The list shows every account with its group
memberships.

Line commands: **S** = edit, **G** = manage this user's group memberships,
**D** = delete (confirm-gated). **PF4** adds a user.

## Adding a user

The Add form takes:

| Field | Rules |
|---|---|
| User ID | required; stored uppercase; must be unique |
| Full name | optional, up to 40 characters |
| Email | optional; basic shape check (one `@`, a dot in the domain) |
| Password / Retype | required, must match; never logged or echoed |

A new user belongs to no groups — use `G` (or the group side,
[Groups](04-groups.md)) to give them membership, or their menu will be empty.

## Editing a user

The Edit form adds these to the fields above:

- **Password** — type a new one to reset it, or leave blank to keep the
  current password. This is the only password-reset mechanism; there is no
  self-service "forgot password" flow, so a locked-out user always comes to
  an admin.
- **MFA req Y/N** — require this user to enroll in TOTP MFA at their next
  login. See [MFA administration](06-mfa.md) for the full semantics.
- **MFA status** (display-only) — `NONE`, `PENDING` (required but not yet
  enrolled), or `ENROLLED`. ⚠️ The status follows the *required* flag: a user
  who **opted in** to MFA themselves (required = N, but enrolled) shows
  `NONE` here even though a secret is stored and verified at login.
- **Clear MFA Y** — type `Y` to wipe the user's enrolled secret (they
  re-enroll at next login if required). Clearing does **not** change the
  MFA-required flag.
- **Lock self Y** — the **User Settings lock**. When `Y`, the user's `0 User
  Settings` menu entry disappears: they cannot change their own password or
  touch their own MFA, and the account is **never force-enrolled** in MFA
  even when MFA req is `Y` (the form shows a note when that combination is
  inert). Intended for shared/guest accounts, where any one holder could
  otherwise enroll a TOTP secret and lock everyone else out. A stored secret
  is still verified at login — the lock freezes MFA as admin-managed, it
  doesn't disable it.

## Deleting a user

`D`, then Enter to confirm. Guardrails: you cannot delete your own account
(`CANNOT DELETE YOUR OWN ACCOUNT`) or the last member of ZZADMIN
(`CANNOT REMOVE LAST ZZADMIN MEMBER`).

## Group membership (`G`)

Shows every group with an `X` next to the ones this user belongs to. Line
commands: **A** = add to group, **R** = remove. The ZZADMIN guardrails apply
here too: you cannot remove your own admin membership, nor the last ZZADMIN
member.

Remember: membership changes affect the user's **next** menu render or login
— an active session keeps its current menu
(see [Concepts](01-concepts.md)).
