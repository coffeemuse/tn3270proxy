# The audit trail

Every security-relevant event is recorded in the database with a UTC
timestamp, the session it belongs to, the **subject** (`username` — the
account the event is about) and the **actor** (who did it; empty before
authentication). Two views: an in-app browser and a CLI.

## In-app: Recent Activity (admin menu option 6)

Shows the last **72 hours**, newest first, capped at the **Audit View Max
Rows** [system parameter](07-system-parameters.md) (the title shows
`(NEWEST n)` when capped — older rows are still in the database, just not in
this snapshot). Failures show red; security state changes show yellow.
Plain Enter refreshes; PF7/PF8 page; line command **S** opens a detail
screen with the full untruncated record (plus a reverse-DNS name for the
client, unless **Audit Reverse DNS** is `N`).

## CLI: `audit list` and `audit prune`

For anything beyond the 72-hour window, scripted queries, and retention:

    tn3270proxy audit list -db proxy.db -kind auth_fail -since 7d -limit 50
    tn3270proxy audit list -db proxy.db -user ALICE          # events about ALICE
    tn3270proxy audit list -db proxy.db -actor ADMIN         # changes ADMIN made
    tn3270proxy audit prune -db proxy.db -older-than 90d     # retention cleanup

Filters AND together. `-user` matches the subject, `-actor` the principal —
"what happened to Alice" vs "what did Alice do". Durations take `h`/`m`/`s`
and a `d` (= 24h) suffix. Pruning is permanent; decide your retention policy
and run it on a schedule (cron), since nothing prunes automatically.

## Event catalog

| Kind | Recorded when | Notes |
|---|---|---|
| `connect` / `disconnect` | a TCP connection opens / closes | detail says how it ended |
| `auth_ok` / `auth_fail` | login success / failure | failures record the attempted username (never the password) and the throttle state (`delay=Xs count=N`) |
| `auth_error` | infrastructure error during login | detail = error text |
| `logout` | a logged-in session returns to the login screen | detail: `user logoff` (PF3) or `idle logout` (timeout) |
| `bridge_start` / `bridge_end` | a backend session begins / ends | service recorded; end detail = cause |
| `mfa_enrolled` / `mfa_success` / `mfa_failed` | enrollment completed / code accepted / code rejected | failures carry throttle detail |
| `mfa_enforced` / `mfa_cleared` | admin set MFA-required / wiped a secret | actor = the admin, username = the subject |
| `password_self` | user changed their own password | via User Settings |
| `settings_locked` / `settings_unlocked` | admin toggled the User Settings lock | actor = the admin, username = the subject |
| `session_disconnect` | admin force-disconnected a session | actor = the admin |
| `admin` | any other admin mutation | detail describes it, e.g. `service delete PROD`, `sysconfig set SYSTEM_ID: PROXY -> MVS1` |
