# TN3270Proxy documentation

User-facing manuals, one folder per audience. Each folder's `README.md` is the
table of contents; topics are individual, focused pages (no monolithic
scroll-forever documents). Screenshots live in each manual's `images/` folder.

## Manuals

| Manual | Audience |
|---|---|
| [Setup / Installation](install/README.md) | Standing up a gateway: Docker quick start, manual install, TLS, MFA key, hardening |
| [Administration](admin/README.md) | Day-to-day operation: users/groups/services, MFA, audit, limits, logging, backups |
| [End-user](user/README.md) | Connecting with a 3270 emulator: login, MFA, the menu, PF3/PA3 navigation |

## For contributors

- [dev/](dev/README.md) — developer-facing reference (ISPF style guide, dependency policy).
  See also [CLAUDE.md](../CLAUDE.md) for repo orientation and conventions.
- `superpowers/` — internal design history (specs, plans, roadmap). Not part of
  the release manuals.
