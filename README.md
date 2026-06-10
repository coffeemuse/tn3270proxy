# TN3270Proxy

[![CI](https://github.com/CoffeeMuse/tn3270proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/CoffeeMuse/tn3270proxy/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/CoffeeMuse/tn3270proxy?sort=semver)](https://github.com/CoffeeMuse/tn3270proxy/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/CoffeeMuse/tn3270proxy)](https://goreportcard.com/report/github.com/CoffeeMuse/tn3270proxy)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](LICENSE.md)

A TN3270 gateway: presents itself as a TN3270 server, authenticates users
(passwords + optional TOTP MFA), shows a group-filtered menu of internal TN3270
services, and bridges the user to the selected service. Administration happens
on the 3270 screen itself, through a built-in ISPF-style admin UI.

This project was created solely to satisfy a personal need. It is released as
open source in the hope that others may find it useful.

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

## Features

See the [FEATURES](FEATURES.md) document for a list of features.

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

## AI Use Disclosure

This project was developed with substantial AI assistance (Anthropic's Claude Code) under a spec-driven workflow. All architecture, design decisions, and feature direction were human-conceived and human-directed; AI-generated code was reviewed, tested, and frequently revised before merging. The full development history — specifications, issues, pull requests, and commits — documents that process and is preserved in this repository. The design decisions in this project are the maintainer's own, and questions about how any part of the system works are welcome. As with all software provided under the GPLv3, this project comes with no warranty; see the LICENSE file for details.
