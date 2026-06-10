# Administration guide

Operating a TN3270Proxy gateway day to day. Administration happens on the
3270 screen itself — members of the `ZZADMIN` group get an `A  Admin` menu
entry — with a small CLI for lifecycle and maintenance tasks.

## Contents

**The model**

1. [Concepts: users, groups, services](01-concepts.md) — the group-filtered
   menu, canonical uppercase names, ZZADMIN, when changes take effect.
2. [The admin UI](02-admin-ui.md) — the admin menu and the conventions every
   screen shares (line commands, PF keys, confirm-gated deletes).

**Day-to-day administration**

3. [Managing users](03-users.md) — accounts, password resets, the MFA fields,
   the User Settings lock, guardrails.
4. [Managing groups](04-groups.md) — membership, the reserved `ZZ*` prefix.
5. [Managing services](05-services.md) — backend hosts, TLS to the backend,
   group access.
6. [MFA administration](06-mfa.md) — the secret-first enforcement model,
   requiring/clearing MFA, the issuer label.
7. [System Parameters](07-system-parameters.md) — runtime settings: system
   ID, MOTD/branding files, auth throttling, audit view options.
8. [Message of the Day](08-motd.md) — authoring the post-login MOTD.

**Platform protection**

9. [Trusted Networks](09-trusted-networks.md) — exempting known networks
   from pre-auth DoS controls.
10. [Connection limits & idle timeouts](10-limits.md) — the `limits` config:
    idle regimes, pre-auth ceiling, connection caps.

**Observability**

11. [Active Sessions](11-sessions.md) — the live session view and
    force-disconnect.
12. [The audit trail](12-audit.md) — the in-app viewer, CLI queries,
    retention, the event catalog.
13. [Logging & external ban scanners](13-logging.md) — the two slog sinks,
    stable auth-failure lines, fail2ban.

**Lifecycle**

14. [CLI reference](14-cli.md) — `serve`, `bootstrap`, `seed`, `audit`,
    `mfa reset-all`, `version`, `quickstart`.
15. [Upgrading & database backups](15-operations-upgrades.md) — migrations,
    automatic pre-upgrade backups, rollback, retention.

The [images/](images/) folder holds any screenshots this guide references.
