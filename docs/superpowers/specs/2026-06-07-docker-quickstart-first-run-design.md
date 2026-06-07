# Docker Quick-Start / First-Run Provisioning — Design

**Date:** 2026-06-07
**Status:** Approved for spec; pending user review of this document
**Scope:** A zero-touch, opinionated first-run experience for the Docker image (and,
as a bonus, bare metal): on a fresh data directory, generate everything needed to
get a hobbyist/demo deployment working end-to-end, with no shipped secrets and no
manual setup commands.

## 1. Purpose & posture

The retro-mainframe hobbyist running emulated MVS 3.8 or VM/370 wants to bridge a
3270 emulator to their box. They are comfortable with 3270 and a sysgen, but not
necessarily with Docker, `docker exec`, or hand-editing config/seed files. Today the
intended on-ramp is multi-step and manual: create files, run `bootstrap` to mint an
admin, run `serve`. That is friction we want to remove for the **quick start**.

This design adds a **low-friction, opinionated quick start**: the user runs exactly
one command — `docker compose up` — and gets a working gateway with an admin account,
two sample users, a demo service that actually bridges, MFA ready, and TLS available.

**Posture — generate-on-first-run, ship-nothing-secret.** We do **not** ship default
passwords, certs, or keys. Everything secret is generated on first run and recorded for
the user. This keeps the spirit of the existing `bootstrap` (a random, surfaced-once
credential) while removing the "you must know to run this command" friction.

**This is explicitly the QUICK START, not the production path.** A separate long-form
"secure it properly" document covers real certs, externally-managed MFA keys, disabling
plaintext, and removing the sample accounts. The quick start is opinionated and
deliberately convenient; the security caveats it accepts (§11) are all things the
long-form doc walks the operator through changing.

### Non-goals

- Not a production deployment path; not hardened by default.
- No changes to the `serve` command's behavior or the `internal/config` schema.
- No force-change-on-first-login flow (noted as a possible future enhancement).
- No interactive prompts — the experience is non-interactive (`docker compose up`).

## 2. Mechanism: a `quickstart` subcommand wired into the image

A new subcommand, `tn3270proxy quickstart -data <dir>`:

- **Idempotent.** On a fresh data dir it provisions everything. On an already-provisioned
  dir it is a no-op that prints a notice and exits 0.
- **Internal plumbing, not a user step.** The Docker image's default command runs it
  automatically before `serve`. The operator never types `quickstart` and never runs
  `docker exec`.

Image command (baked into the image, not the compose file the user edits):

```
tn3270proxy quickstart -data /data && tn3270proxy serve -config /data/tn3270proxy.json
```

(Equivalently a tiny entrypoint script performing the same two steps.)

### Why a subcommand and not auto-provisioning inside `serve`

Folding sample-seeding into `serve` would give `serve` a hidden "secretly create sample
users" mode that could fire in a production deployment. Keeping it a separate, explicit,
neutral subcommand means **`serve` stays honest** — it never auto-seeds. The opinionated
quick-start lives in the **Docker image** (which wires `quickstart && serve` together),
not in the binary's default behavior. The operator experience is identical either way:
one `docker compose up`, zero edits, zero `exec`.

### Near-zero blast radius

`serve` and `internal/config` require **no changes**. `quickstart` generates a
`tn3270proxy.json` that the existing config loader already understands — it already has
`db`, `listeners.plain`, `listeners.tls.{enabled,addr,cert,key}`, and `mfa.key_file`
(see `internal/config/config.go` `fileConfig`). All new logic is confined to the
`quickstart` subcommand plus a self-signed-cert helper. It reuses the existing `store`,
`seed`, `auth`, `mfa`, and `sysconfig` packages.

## 3. Data-directory layout (the single bind mount)

Docker Compose bind-mounts one host directory to `/data`. `quickstart` owns it:

```
/data/
  tn3270proxy.json        # generated config — ALSO the "provisioned" marker (written LAST)
  proxy.db                # SQLite database (auto-migrated by store.Open)
  proxy.db-wal            # WAL companion
  proxy.db-shm            # WAL companion
  mfa.key                 # base64 of a 32-byte AES-256 master key      (mode 0600)
  tls/
    cert.pem              # self-signed: CN/SAN localhost, 127.0.0.1, ::1
    key.pem              # private key                                  (mode 0600)
  motd.txt                # default MOTD/NEWS welcome text
  SETUP-DEFAULTS.TXT       # human-readable record of everything generated
```

All paths inside the generated `tn3270proxy.json` are absolute under `/data`, so `serve`
resolves them regardless of working directory.

## 4. Fresh-vs-existing detection

- **Provisioned marker = `tn3270proxy.json` exists.** `quickstart` writes this file
  **last**, after every other artifact has been created successfully. Its presence
  therefore means "the data dir is fully provisioned."
- **Existing** (`tn3270proxy.json` present): print
  `Existing installation detected at /data — leaving it untouched.` and exit 0 without
  modifying anything.
- **Fresh** (`tn3270proxy.json` absent): provision. Because the config file is written
  last, a crash partway through leaves the dir without the marker, so the next boot
  re-runs provisioning. Every step is individually idempotent and safe to re-run:
  - `store.Open` runs an idempotent migration.
  - The cert/key, `mfa.key`, and `motd.txt` are written only if absent.
  - `seed.Apply` uses `INSERT OR IGNORE` (idempotent).
  - `SETUP-DEFAULTS.TXT` is rewritten on each fresh provisioning pass.

  This makes a partial provisioning resume cleanly rather than corrupting state or
  duplicating rows.

## 5. What gets generated

### Admin account
Reuse the existing `bootstrap` approach: a random one-time password from the 3270-safe
charset (`ABCDEFGHJKMNPQRSTVWXYZ23456789` — excludes `I/L/O/0/1` to avoid transcription
errors on a physical 3270 keyboard), formatted `XXXX-XXXX-XXXX`, bcrypt-hashed via
`auth.HashPassword`. User `ADMIN`, member of `ZZADMIN`.

### Two sample users
`OPERATOR` and `GUEST`, each with its **own** randomly generated 3270-safe password
(no shared "changeme" — consistent with ship-no-default-passwords). Both placed in the
sample group `DEMO`, **not** `ZZADMIN`.

### Sample group + demo service
- Group `DEMO`.
- Service `DEMO` → host `dummy3270`, port `3300`, **plaintext** (no TLS). This is the
  sibling compose container (§8): a stateless throwaway TN3270 server that paints an
  obviously-fake welcome screen and tells the bridged user to press PA3 to return.
- Service linked to the `DEMO` group; `ADMIN` is **also** added to `DEMO` so the admin's
  very first login already shows a working demo entry.
- Result: every seeded account (`ADMIN`, `OPERATOR`, `GUEST`) sees one menu item that
  bridges to `dummy3270` out of the box — a working connect → login → menu → bridge →
  PA3-back loop with zero configuration.

### MFA master key
32 `crypto/rand` bytes, base64-encoded, written to `/data/mfa.key` (mode 0600) and
referenced by the generated config's `mfa.key_file`. MFA stays **opt-in**: no sample
account is enrolled, so `serve`'s `mfaStartup` simply seals the `MFA_KEY_CHECK` sentinel
on first boot (it tolerates `enrolled == 0` with a key present). MFA is *ready* for any
user to enroll via User Settings, but not forced on anyone.

### Self-signed TLS certificate
Generated in Go (`crypto/x509` + `crypto/tls`); **no `openssl` dependency**. Subject/SAN
covers `localhost`, `127.0.0.1`, and `::1`. Validity 825 days. Written to
`/data/tls/cert.pem` and `/data/tls/key.pem` (key mode 0600). Connecting from another
host by IP will not match the SAN — acceptable for the quick start; the long-form doc
covers real certs.

### MOTD/NEWS file
`/data/motd.txt` (§7), with the `sysconfig` `motd_file` key set to point at it so `serve`
renders it on login.

### Generated config (`tn3270proxy.json`)
```json
{
  "db": "/data/proxy.db",
  "listeners": {
    "plain": { "enabled": true, "addr": ":2323" },
    "tls":   { "enabled": true, "addr": ":2324",
               "cert": "/data/tls/cert.pem", "key": "/data/tls/key.pem" }
  },
  "mfa": { "key_file": "/data/mfa.key" }
}
```
Both listeners are enabled — the frictionless plaintext path *and* a live TLS port one
over. An admin may later disable either; this is the opinionated quick-start default.

## 6. `SETUP-DEFAULTS.TXT`

The durable, Docker-fluency-free record of everything generated. Because the data dir is
bind-mounted, the user opens this file with an ordinary file browser — no `docker logs`
or `docker exec` required. A loud banner is *also* printed to the container logs as
belt-and-suspenders, but the file is the system of record. Contents:

- A "FRESH INSTALL — quick-start defaults generated" header with a timestamp.
- **ADMIN** username + one-time password.
- The two sample users (`OPERATOR`, `GUEST`) + their passwords + their group (`DEMO`).
- How to connect: `host:2323` (plaintext) and `host:2324` (TLS, self-signed — expect a
  trust prompt in the emulator).
- A note that the `DEMO` menu entry bridges to the throwaway `dummy3270` service and that
  PA3 returns to the menu.
- MFA note: enabled and opt-in; **back up `mfa.key`** — losing it loses the ability to
  decrypt any future enrollments.
- A prominent **"CHANGE THESE PASSWORDS"** block.
- A prominent **"This is the QUICK START, not a production setup — see `<security-doc>`
  to harden this deployment."**
- "You can delete this file once you have recorded the credentials."

No password is ever stored in plaintext in the database or logs — only this file (which
the user is told to delete) and the bcrypt hashes in the DB.

## 7. MOTD/NEWS contents

A short welcome rendered on the first 3270 login, following the existing chrome-less
TSO/READY-style `internal/screens/news.go` convention (no title row, no PF-key help row,
red protected text, `***` page gate). It:

- Welcomes the admin.
- States this is a quick-start demo deployment.
- **Strongly encourages changing the ADMIN and sample passwords** immediately.
- Points at the security document for hardening.

## 8. `docker-compose.yml` (shipped; no edits needed)

Two services on a shared internal network:

- **`tn3270proxy`** — our image; `ports: 2323:2323` and `2324:2324`; `volumes:
  ./data:/data`; the `quickstart && serve` command baked into the image. This is the only
  service exposing published ports.
- **`dummy3270`** — the **same image**, run with a different command
  (`dummy3270 -listen :3300`); **no published ports**; reachable by the proxy at
  `dummy3270:3300` via Docker's service-name DNS on the internal network.

Because the demo backend is a container we control on the internal network, the seeded
`DEMO` service works out of the box without any assumption about the hobbyist's own
topology — and exposes no extra attack surface to the host.

## 9. Dockerfile

A multi-stage Go build producing **both** binaries — `tn3270proxy` and `dummy3270` — into
one minimal runtime image. The two compose services run the same image with different
commands. (No `openssl` or other runtime tooling is required, since cert generation is
pure Go.)

## 10. Testing

### Unit tests (`quickstart`)
- Fresh dir → every artifact is created (config, db, mfa.key, cert/key, motd, SETUP file).
- Re-run on a provisioned dir → no-op, no mutation, exit 0.
- Partial dir (artifacts present, config absent) → resumes and completes without
  duplicating rows.
- The generated `tn3270proxy.json` round-trips through `config.Load` without error.
- The generated cert is a valid X509 leaf and is loadable by `listen.Build`
  (`tls.LoadX509KeyPair`).
- The passwords written to `SETUP-DEFAULTS.TXT` verify against the stored bcrypt hashes.
- `seed` produces exactly the expected users, groups, group memberships, and the `DEMO`
  service linked to the `DEMO` group with `ADMIN` also in `DEMO`.
- `mfa.key` decodes to 32 bytes; a subsequent `serve` seals the sentinel and starts.

### Smoke test (extend the s3270-smoke-testing skill)
`docker compose up` → connect → log in as `ADMIN` → select the `DEMO` entry → land on a
`dummy3270` welcome screen → press PA3 → return to the menu.

## 11. Security caveats (documented; accepted for quick-start)

All four are appropriate for a hobbyist/demo LAN and are exactly what the long-form
"secure it properly" document walks the operator through changing:

1. **`mfa.key` sits next to the database it encrypts.** Disk compromise that reads the DB
   can also read the key. Production sources the key from an environment variable or
   secret store, not the data dir.
2. **Self-signed cert for `localhost`.** No real trust chain; emulators prompt. Production
   uses a real certificate.
3. **Generated passwords live in a host-side plaintext file.** Mitigated by telling the
   user to delete `SETUP-DEFAULTS.TXT` after recording them.
4. **Plaintext `:2323` is enabled.** Convenient for a LAN; production disables it and uses
   TLS only.

## 12. Open defaults (chosen, overridable)

These were chosen as sensible defaults during design and can be adjusted before
implementation without changing the architecture:

- Sample user names: `OPERATOR`, `GUEST`.
- Sample group name: `DEMO`; demo service name: `DEMO`.
- `ADMIN` is added to the `DEMO` group so the admin sees the demo service on first login.
- Self-signed cert validity: 825 days.
- TLS quick-start port: `:2324`.
