# First run: bootstrap or seed

## Bootstrap (first admin account)

A fresh database has no admin, so the admin UI (menu entry `A`) is unreachable. Create the
first admin account with:

    ./bin/tn3270proxy bootstrap -db proxy.db

This prints a one-time password for user `ADMIN` (crypto/rand, never a fixed default). Log
in as `ADMIN`, create your real admin account via the admin UI, then delete `ADMIN`. The
command refuses with an error if any admin already exists, so it is safe to re-run; use the
admin UI for all subsequent account management.

## Seed users / groups / services / trusted networks (optional)

Seeding is a one-time convenience for bulk-loading initial data on a fresh installation.
It is not required — a fresh system is fully configurable from the admin UI after running
`bootstrap`. **Seed and bootstrap are mutually exclusive:** use one or the other to populate
the first admin, not both. Re-running `seed` against a populated database is an error; use
the admin UI for subsequent account and service management.

    ./bin/tn3270proxy seed -db proxy.db -file seed.example.json

See [`seed.example.json`](../../seed.example.json) for the format. If the seed file
contains no ZZADMIN member, `serve` and `seed` will both warn that the admin UI is
unreachable and point at `bootstrap`.

The optional `trusted_networks` list pre-provisions
[trusted networks](../admin/09-trusted-networks.md) (admin menu option 5), so a scripted
install doesn't have to write to the database behind the gateway's back. A typical
case is a local terminal controller that connects from `127.0.0.1` and must not be
dropped by the pre-auth timers while terminals sit at the login screen:

    "trusted_networks": [
      { "cidr": "127.0.0.1",    "comment": "Local terminal controller" },
      { "cidr": "10.20.0.0/24", "comment": "Machine-room console LAN" }
    ]

`cidr` takes a bare IP (stored as a host route, `/32` or `/128`) or a prefix (stored
masked, so `10.20.0.9/24` becomes `10.20.0.0/24`); `comment` is required, max 40
characters. Every entry is validated before anything is written: an invalid CIDR, a
missing comment, the same network listed twice, or a network already in the database
fails the whole seed with no users, groups, or services created.
