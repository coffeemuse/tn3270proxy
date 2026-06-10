# Setup / Installation guide

How to stand up a TN3270Proxy gateway — from nothing to a verified login over
a 3270 emulator.

## Choosing an install path

There are three ways to get the gateway, in decreasing order of convenience:

1. **[Docker quick start](01-quickstart.md)** — one `docker compose up`
   provisions a complete demo gateway (admin account, sample users, a DEMO
   backend, TLS, MFA key). Turnkey end-to-end; start here to evaluate.
2. **[Binary release](02-binary-releases.md)** — download a prebuilt,
   checksummed binary from GitHub Releases. No toolchain needed.
3. **[From source](03-from-source.md)** — `go build`, for development or
   platforms without a prebuilt binary.

Paths 2 and 3 differ only in how you obtain the binary; both continue through
the same setup flow below. Docker users who outgrow the quick start's
generated defaults follow the same pages (see
[running without the quick-start wrapper](08-security-hardening.md)).

## Setting up

4. **[First run: bootstrap or seed](04-first-run.md)** — create the first
   admin account, or bulk-load users/groups/services from JSON.
5. **[Running the gateway](05-running.md)** — `serve`, the JSON config file,
   plaintext/TLS listeners, self-signed certs for testing.
6. **[The MFA master key](06-mfa-key.md)** — generating and supplying the
   key, fail-closed startup checks, break-glass reset.
7. **[Running as a service](07-running-as-a-service.md)** — layout,
   permissions, an example systemd unit, logs, upgrades.
8. **[Securing tn3270proxy](08-security-hardening.md)** — the
   before-production checklist: real credentials, real TLS, key placement,
   disabling plaintext, non-root.
9. **[Verifying the install](09-verify.md)** — version, listener, login,
   bridge; common first-connection problems.

Screenshots referenced by this guide live in [images/](images/).
