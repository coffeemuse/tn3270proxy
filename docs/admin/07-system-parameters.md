# System Parameters

Admin menu option **4** — runtime settings stored in the database and edited
on one form. Enter validates **all** fields before saving **any** (a bad
value blocks the whole save); each effective change is audited. The form
stays on screen after a save; PF3 returns to the admin menu.

No restart is ever needed — every parameter is read at use time.

| Parameter | Default | What it does |
|---|---|---|
| System ID | `PROXY` | Identifier in the menu's status block. A–Z/0–9/dash, max 7. Effective next render. |
| MOTD File | *(empty)* | Absolute path to the post-login MOTD/NEWS text. Empty disables. Read fresh each login — see [MOTD](08-motd.md). |
| Branding File | *(empty)* | Absolute path to login-screen branding art. Empty shows the default. Read fresh each login paint — see below. |
| Auth Delay Base (sec) | `2` | Failed-auth backoff base. `0` disables throttling. |
| Max Auth Tries | `5` | Failure count where the backoff stops growing. |
| Auth Fail Window (min) | `15` | Idle period after which a username's failure count decays. |
| MFA Issuer | `TN3270PROXY` | Label in authenticator apps. No colons, max 40 — see [MFA](06-mfa.md). |
| Audit View Max Rows | `1000` | Cap on the in-app audit snapshot (1–10000) — see [Audit](12-audit.md). |
| Audit Reverse DNS | `Y` | `N` skips PTR lookups on audit/session detail screens (for air-gapped or egress-locked deployments). |

(The database also holds an internal `MFA_KEY_CHECK` row used by the
startup key check — it is deliberately not on this form. Leave it alone.)

## Auth throttling

The three Auth parameters implement per-username linear backoff shared by
the password and MFA failure paths:

    delay = AUTH_DELAY_BASE_SECS × min(failure count, AUTH_MAX_TRIES)

With the defaults, a 5th consecutive failure waits 10 seconds, and it never
grows past that. Counts reset on success and decay after the fail window.
Keying is by **username**, not source IP — so an attacker rotating IPs gains
nothing, and users behind a shared NAT don't throttle each other. The applied
delay is recorded in the `auth_fail` / `mfa_failed` audit detail
(`delay=Xs count=N`). This is backoff, not lockout: accounts never lock, so
pair it with an external banner like fail2ban if you want bans — see
[Logging](13-logging.md).

## Branding file authoring

The branding file is plain text painted verbatim on the login screen's body
(between the status header and the credential fields):

- Absolute path only; up to 8 KiB read per paint.
- Lines render from column 0 and are clipped at the screen edge; extra lines
  beyond the body region are dropped, shorter art is vertically centered.
- Read fresh on every login paint — edits show up immediately, and an
  unreadable file just falls back to the default art.
