# CLI reference

All administration of *content* (users, groups, services, parameters)
happens in the 3270 admin UI; the CLI covers lifecycle and maintenance.
Every subcommand takes `-db` (default `tn3270proxy.db`).

## `serve` — run the gateway

    tn3270proxy serve -config /etc/tn3270proxy/tn3270proxy.json

| Flag | Meaning |
|---|---|
| `-config` | JSON config file (default: `tn3270proxy.json` in the working directory, if present) |
| `-db` | SQLite database path (overrides config) |
| `-listen` | plaintext listen address (overrides config; error if the config disabled the plain listener) |
| `-pre-auth-idle`, `-pre-auth-max`, `-idle`, `-max-conns`, `-max-per-ip` | override the corresponding [limits](10-limits.md) |
| `-log-level`, `-log-file` | logging overrides — see [Logging](13-logging.md) |

Flags beat config-file values, which beat built-in defaults. The MFA master
key comes from the environment or config — see
[the install guide](../install/06-mfa-key.md).

## `bootstrap` — create the first admin

    tn3270proxy bootstrap -db proxy.db

Prints a one-time generated password for a new `ADMIN` account in ZZADMIN.
Refuses if an admin already exists. Log in, create your real account, delete
`ADMIN`.

## `seed` — bulk-load initial data

    tn3270proxy seed -db proxy.db -file seed.json

One-time convenience for a fresh database; fails loudly on conflicts. Use
the admin UI for everything afterwards.

## `audit list` / `audit prune` — query and trim the audit trail

See [The audit trail](12-audit.md) for filters and retention guidance.

## `mfa reset-all` — break-glass MFA wipe

    TN3270PROXY_MFA_KEY=<key> tn3270proxy mfa reset-all -db proxy.db

Clears **every** user's MFA enrollment and re-stamps the key sentinel — the
recovery path for a lost master key. See
[the install guide](../install/06-mfa-key.md).

## `version` — print the build version

Release builds print their tag; source builds print a commit hash.

## `quickstart` — provision a demo data directory

    tn3270proxy quickstart -data /data

The Docker first-run provisioner ([install guide](../install/01-quickstart.md)).
Idempotent: no-ops when the directory is already provisioned. Not the
production path.
