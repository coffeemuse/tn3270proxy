# TN3270Proxy

A TN3270 gateway: presents itself as a TN3270 server, authenticates users,
shows a group-filtered menu of internal TN3270 services, and bridges the user
to the selected service. See `docs/superpowers/specs/` for the design.

## Build

    go build -o bin/tn3270proxy ./cmd/tn3270proxy

For a release build with version stamping (replaces the `dev` default):

    go build -ldflags "-X main.version=v1.2.3" -o bin/tn3270proxy ./cmd/tn3270proxy

Print the resolved version:

    ./bin/tn3270proxy version

Plain `go build` without ldflags still reports a useful version — a short VCS commit
hash from `runtime/debug.ReadBuildInfo`, with a `-dirty` suffix when the working tree
has uncommitted changes.

## Bootstrap (first admin account)

A fresh database has no admin, so the admin UI (menu entry `A`) is unreachable. Create the
first admin account with:

    ./bin/tn3270proxy bootstrap -db proxy.db

This prints a one-time password for user `ADMIN` (crypto/rand, never a fixed default). Log
in as `ADMIN`, create your real admin account via the admin UI, then delete `ADMIN`. The
command refuses with an error if any admin already exists, so it is safe to re-run; use the
admin UI for all subsequent account management.

## Seed users / groups / services (optional)

Seeding is a one-time convenience for bulk-loading initial data on a fresh installation.
It is not required — a fresh system is fully configurable from the admin UI after running
`bootstrap`. **Seed and bootstrap are mutually exclusive:** use one or the other to populate
the first admin, not both. Re-running `seed` against a populated database is an error; use
the admin UI for subsequent account and service management.

    ./bin/tn3270proxy seed -db proxy.db -file seed.example.json

See `seed.example.json` for the format. If the seed file contains no ZZADMIN member, `serve`
and `seed` will both warn that the admin UI is unreachable and point at `bootstrap`.

## Run

    ./bin/tn3270proxy serve -db proxy.db -listen :2323

Connect with any TN3270 emulator (e.g. `c3270 host:2323`). Press **PA3** during
a bridged session to return to the menu; **PF3** at the menu disconnects.

### Configuration file

Richer setup (e.g. TLS) uses a JSON config file. By default `serve` looks for
`tn3270proxy.json` in the working directory; pass `-config <path>` to choose
another. Flags (`-listen`, `-db`) override file values, which override built-in
defaults. `tn3270proxy.example.json` is a ready-to-copy starter (TLS off by
default). For example, to enable both listeners at once:

    {
      "db": "tn3270proxy.db",
      "listeners": {
        "plain": { "enabled": true,  "addr": ":2323" },
        "tls":   { "enabled": true,  "addr": ":3270",
                   "cert": "server.crt", "key": "server.key" }
      }
    }

Both listeners are independent: enable either or both. TLS is terminated
immediately on connect (not STARTTLS), as TN3270-over-TLS clients expect.
Unknown keys in the config file are rejected, so a typo fails loudly rather
than silently disabling a listener.

Generate a self-signed cert/key for local testing (gitignored):

    openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
      -nodes -keyout server.key -out server.crt -days 365 \
      -subj "/CN=localhost" -addext "subjectAltName=IP:127.0.0.1,DNS:localhost"

## Logging

The proxy logs to two concurrent sinks via slog:

- **stderr** — human-readable text, always on (the friendlier console/journald stream).
- **a JSON file** — machine-readable, enabled with `-log-file /path/to/file.json`
  (or `log.file` in the config). Both sinks share the level (`-log-level`,
  default `info`) and carry identical fields.

### Auth-failure lines (fail2ban-friendly)

Every authentication failure — wrong password or wrong TOTP code — emits a
**stable** line you can wire an external scanner (e.g. fail2ban) to. The proxy
does not ship or run fail2ban; it only provides the log surface.

JSON (file sink):

    {"time":"...","level":"WARN","msg":"auth failed","remote":"203.0.113.7:51324","user":"alice","src":"203.0.113.7","trusted":false,"reason":"invalid_credentials"}

Text (stderr sink):

    level=WARN msg="auth failed" remote=203.0.113.7:51324 user=alice src=203.0.113.7 trusted=false reason=invalid_credentials

`src` is the bare client IP (the fail2ban `<HOST>`), `trusted` reflects the
admin trusted-network list (managed in the admin UI under Trusted Networks,
stored in the database), and `reason` is coarse (`invalid_credentials` or
`bad_mfa`) — the on-screen message stays uniform, so the reason never reveals
whether a username exists. The field set is a **stable contract**: filters
depend on it.

> Auth throttling is per-username backoff, not a hard lockout, so repeated
> failures keep emitting these lines; let your scanner's own retry counter
> (fail2ban `maxretry` / `findtime`) decide when to ban. Match `trusted=false`
> so trusted sources are never banned.

### Example fail2ban filter (adapt to your deployment)

Point fail2ban at the JSON log file:

    # /etc/fail2ban/filter.d/tn3270proxy.conf
    [Definition]
    failregex = "msg":"auth failed".*"src":"<HOST>".*"trusted":false
    ignoreregex =

Or, tailing the stderr/journald text stream:

    failregex = msg="auth failed" .*\bsrc=<HOST>\b.*\btrusted=false\b

with a jail like:

    # /etc/fail2ban/jail.d/tn3270proxy.conf
    [tn3270proxy]
    enabled  = true
    filter   = tn3270proxy
    logpath  = /var/log/tn3270proxy/auth.json
    maxretry = 5
    findtime = 10m
    bantime  = 1h

## Message of the Day (MOTD / NEWS)

Set the **MOTD File** system parameter (admin menu → System Parameters) to the
**absolute path** of a plain-text file. After a successful login, its contents
are shown in red, one page at a time, before the menu — press **ENTER** to page
through (PA3 and PF3 do nothing here). Leave the parameter empty to disable it.

The file is read **fresh on every login**, so edits take effect on the next
sign-on — no restart. Authoring rules:

- **Absolute path only.** A relative path is ignored (logged, then skipped).
- **Fixed width: lines truncate at column 79** ("80-column record length"). The
  file is a composed banner — you own the line breaks and indentation; anything
  past column 79 is dropped. Compose to fit.
- **Keep it short** — realistically no more than ~3 screens; longer notices tend
  to get paged past unread. Files larger than 8 KiB are clipped.
- Write maintenance windows in **UTC** by convention.

## Status

MVP + TLS inbound listener. Not yet implemented (see spec section 9 / ROADMAP):
backend TLS, admin UI, TN3270E, audit logging, session multiplexing.

## Test

    go test ./...
