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

## `doc import` / `doc export` — manage document content

    tn3270proxy doc import -db proxy.db -name MOTD     -file /etc/motd.txt
    tn3270proxy doc import -db proxy.db -name BRANDING -file art.txt
    tn3270proxy doc export -db proxy.db -name BRANDING -file art.txt

The CLI sibling of the admin Documents import/export screens — handy for
scripted provisioning, Docker builds, or CI pipelines when nobody is at a 3270.

| Flag | Meaning |
|---|---|
| `-name` | Document to operate on: `MOTD` or `BRANDING` (case-insensitive; required) |
| `-file` | Path to read from (import) or write to (export; required) |
| `-force` | (export only) Overwrite the destination file if it already exists |

Import reads the file from the local filesystem (the host running the CLI,
not the server path used by the admin import form) and replaces the document's
entire content. Files over 8 KiB are rejected. The import is audited as
`doc_import` (actor `cli`). Export writes the current document content to a
file; without `-force` it refuses to overwrite an existing file. Export is
read-only and does not write an audit record.

## `version` — print the build version

Release builds print their tag; source builds print a commit hash.

## `quickstart` — provision a demo data directory

    tn3270proxy quickstart -data /data

The Docker first-run provisioner ([install guide](../install/01-quickstart.md)).
Idempotent: no-ops when the directory is already provisioned. Not the
production path.
