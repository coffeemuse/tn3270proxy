# First run: bootstrap or seed

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

See [`seed.example.json`](../../seed.example.json) for the format. If the seed file
contains no ZZADMIN member, `serve` and `seed` will both warn that the admin UI is
unreachable and point at `bootstrap`.
