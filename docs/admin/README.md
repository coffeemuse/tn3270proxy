# Administration guide

Operating a TN3270Proxy gateway day to day: users, groups, services, MFA,
system parameters, sessions, the audit trail, connection limits, logging, and
backups/upgrades.

> **Status:** under construction
> ([#104](https://github.com/CoffeeMuse/TN3270Proxy/issues/104)). The pages
> below are the current content; the full guide is being authored topic by
> topic.

## Contents

1. [Logging & external ban scanners](logging.md) — the two slog sinks,
   stable auth-failure lines, an example fail2ban filter.
2. [Message of the Day (MOTD / NEWS)](motd.md) — the MOTD File system
   parameter and authoring rules.
3. [Upgrading & database backups](operations-upgrades.md) — what happens on
   upgrade (schema migrations, automatic pre-migrate backups), rolling back,
   on-demand backups, retention.

Screenshots referenced by this guide live in [images/](images/).
