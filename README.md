# TN3270Proxy

[![CI](https://github.com/CoffeeMuse/tn3270proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/CoffeeMuse/tn3270proxy/actions/workflows/ci.yml)

A TN3270 gateway: presents itself as a TN3270 server, authenticates users
(passwords + optional TOTP MFA), shows a group-filtered menu of internal TN3270
services, and bridges the user to the selected service. Administration happens
on the 3270 screen itself, through a built-in ISPF-style admin UI.

## Quick start

The fastest path is the Docker quick start — one `docker compose up` provisions
a demo gateway (admin account, sample users, a DEMO backend, TLS, MFA key) you
can connect to with any 3270 emulator:

    c3270 127.0.0.1:2323

See the [Quick Start guide](docs/install/01-quickstart.md). Prefer a plain
binary? Grab a [prebuilt release](docs/install/02-binary-releases.md) or
[build from source](docs/install/03-from-source.md), then follow
[First run](docs/install/04-first-run.md) and
[Running the gateway](docs/install/05-running.md).

## Documentation

| Manual | Audience |
|---|---|
| [Setup / Installation](docs/install/README.md) | Standing up a gateway: Docker quick start, building, TLS, hardening |
| [Administration](docs/admin/README.md) | Day-to-day operation: users/groups/services, MFA, audit, logging, backups |
| [End-user](docs/user/README.md) | Connecting with a 3270 emulator: login, MFA, the menu, PF3/PA3 navigation |

Contributor-facing reference lives in [docs/dev/](docs/dev/README.md).

## Releases

Tagged releases (`vX.Y.Z`) publish cross-platform binaries to the GitHub
Releases page and multi-arch Docker images to `ghcr.io/coffeemuse/tn3270proxy`.
See [Installing from a binary release](docs/install/02-binary-releases.md).
