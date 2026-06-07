# Quick Start (Docker)

The fastest way to try tn3270proxy. **Not** a production setup — see
[security-hardening.md](security-hardening.md) before exposing it.

## Run it

```bash
mkdir -p data
docker compose up -d
```

On first boot the container provisions a fresh `./data` directory and writes
`./data/SETUP-DEFAULTS.TXT` with your generated **ADMIN** password and two sample
users. Open that file to get your login.

## Connect

Point a 3270 emulator at your host:

- Plaintext: `<host>:2323`
- TLS (self-signed, expect a trust prompt): `<host>:2324`

```bash
c3270 127.0.0.1:2323
```

Log in as `ADMIN`. The menu shows a **DEMO** entry that bridges to a throwaway
`dummy3270` backend (a sibling container). Select it to see the bridge work; press
**PA3** to return to the menu.

## First things to do

1. Change the ADMIN and sample passwords (log in, press `A` for the admin UI).
2. Delete `./data/SETUP-DEFAULTS.TXT` once you've recorded the credentials.
3. Point a real service at your own MVS/VM host via the admin UI.
4. Read [security-hardening.md](security-hardening.md) before going beyond your LAN.

## What got generated

Everything lives in the bind-mounted `./data` directory: the SQLite database, the
MFA master key (`mfa.key` — back it up), the self-signed TLS cert (`tls/`), the
MOTD (`motd.txt`), and the generated config (`tn3270proxy.json`). Subsequent
`docker compose up` runs detect the existing install and leave it untouched.
