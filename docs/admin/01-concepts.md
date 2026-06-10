# Concepts: users, groups, services

Three kinds of objects, all managed from the 3270 admin UI:

- A **user** logs in with a username and password (and optionally TOTP MFA).
- A **group** connects users to services. A user sees a service on their menu
  only when **some group they belong to has been granted access to it** — the
  menu is group-filtered, so two users can log in and see entirely different
  service lists.
- A **service** is a backend TN3270 host (name, description, host:port,
  optional TLS) the gateway can bridge a user to.

## Canonical uppercase names

Usernames, group names, and service names are **folded to uppercase** when
created and matched case-insensitively everywhere — `alice`, `Alice`, and
`ALICE` are the same account. Passwords and service host names are never
case-folded.

Service names are short identifiers: A–Z and 0–9 only, at most 8 characters.
Every service also carries a free-form **description** (up to 40 characters,
mixed case) — that's the user-facing label on the menu.

## The ZZADMIN group and the ZZ* reserved prefix

Group names beginning with `ZZ` are **reserved for the gateway itself**. The
admin UI will not create or delete them; you can only manage their
membership.

`ZZADMIN` is the one that matters: its members get the `A  Admin` menu entry
and full access to the admin UI. Three guardrails prevent locking yourself
out:

- You cannot delete your own account.
- You cannot remove your own ZZADMIN membership.
- You cannot delete or demote the **last** ZZADMIN member — the group can
  never become empty.

## When changes take effect

Admin changes apply at the **next menu render or login** — live sessions are
not re-evaluated:

- Revoking a group or deleting a user does **not** kick an active session.
  The user keeps their current menu until they log in again (or you
  disconnect them from the [Active Sessions screen](11-sessions.md)).
- Granting access shows up the next time the user's menu paints.
- System parameters are read at use time (next login, next render, next auth
  attempt) — no restart needed.
- Trusted-network changes apply to **new connections** immediately.
