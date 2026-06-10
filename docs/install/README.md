# Setup / Installation guide

How to stand up a TN3270Proxy gateway — from nothing to a verified login over
a 3270 emulator.

> **Status:** under construction
> ([#103](https://github.com/CoffeeMuse/TN3270Proxy/issues/103)). The pages
> below are the current content; the full guide (requirements, manual install,
> listeners/TLS, MFA key provisioning, running as a service, install
> verification) is being authored topic by topic.

## Contents

1. [Quick Start (Docker)](quickstart.md) — fastest path to a running gateway:
   the `quickstart` provisioner, what it generates, first connection.
2. [Building from source & release artifacts](build.md) — `go build`,
   version-stamped release builds, GitHub Releases binaries, GHCR images.
3. [First run: bootstrap or seed](first-run.md) — creating the first admin
   account, optional bulk seeding from JSON.
4. [Running the gateway](running.md) — `serve`, the JSON config file,
   plaintext/TLS listeners, self-signed certs for testing.
5. [Securing tn3270proxy](security-hardening.md) — beyond the quick start:
   real credentials, a real TLS certificate, MFA key placement, disabling
   plaintext, running as non-root, and running without the quickstart wrapper.

Screenshots referenced by this guide live in [images/](images/).
