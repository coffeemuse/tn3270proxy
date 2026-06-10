# Active Sessions

Admin menu option **7** — a live view of every current connection. Plain
Enter refreshes the snapshot; PF7/PF8 page.

Columns: session **ID** (a process-lifetime serial), **CLIENT** address,
**CONNECTED** time and elapsed **SESSION** duration, **USER** (`(login)` for
connections still at the login screen; `*YOU*` marks your own session), and
**SERVICE** (`-` unless currently bridged to a backend).

## Session detail (`S`)

Line command `S` opens the detail screen: full client address, optional
reverse-DNS name (controlled by the **Audit Reverse DNS**
[system parameter](07-system-parameters.md)), connect and login timestamps
(UTC, with Julian date), user, and bridged service.

## Disconnecting a session (PF11)

From the detail screen, **PF11** force-disconnects — press it twice:

1. First PF11 shows `CONFIRM DISCONNECT <user/address> - PRESS PF11 AGAIN`.
2. Second PF11 hard-closes the connection. The client's emulator drops; the
   action is audited as `session_disconnect` with you as the actor.

You cannot disconnect your own session.

This is the tool to reach for when an admin change must take effect *now*:
since group/service changes only apply at the user's next login
([Concepts](01-concepts.md)), disconnect the session and let the user log
back in under the new rules.
