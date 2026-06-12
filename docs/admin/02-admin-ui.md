# The admin UI

Members of `ZZADMIN` see an extra `A  Admin` entry on the service menu.
Selecting it opens the **TN3270 GATEWAY ADMIN** menu:

| Option | Screen | Covered in |
|---|---|---|
| 1 | Users — accounts and group membership | [Users](03-users.md) |
| 2 | Groups — group definitions | [Groups](04-groups.md) |
| 3 | Services — backend TN3270 services | [Services](05-services.md) |
| 4 | Sysparms — runtime system parameters | [System Parameters](07-system-parameters.md) |
| 5 | Networks — trusted networks (DoS allow-list) | [Trusted Networks](09-trusted-networks.md) |
| 6 | Audit — browse the audit trail | [Audit trail](12-audit.md) |
| 7 | Sessions — active client sessions | [Active Sessions](11-sessions.md) |
| 8 | Documents — MOTD, login branding, and help text | [Documents](08-motd.md) |

Type the option number at `Option ===>` and press Enter. PF3 returns to the
service menu.

## Conventions shared by every admin screen

**Lists.** Rows have a `CMD` input column on the left. Type a single-letter
**line command** next to a row and press Enter — `S` (select/edit) and `D`
(delete) are the common ones; each screen documents its own. An unknown
letter shows `INVALID COMMAND`. **PF7/PF8** page up/down, **PF4** opens the
Add form on screens that create things, and plain **Enter** refreshes
snapshot screens (sessions, audit).

**Delete is confirm-gated.** Typing `D` doesn't delete — the message line
shows `ENTER = CONFIRM DELETE OF '<name>', PF3 = CANCEL`. Press Enter to
commit, PF3 to back out.

**Forms.** Add/Edit screens are fill-in forms; Enter validates and saves,
showing the first problem on the red message line with your input preserved.
**PF3 cancels without saving** — on every form, list, and detail screen, PF3
uniformly steps back one level.

**Auditing.** Every mutation made through the admin UI is recorded in the
[audit trail](12-audit.md) with you as the actor.
