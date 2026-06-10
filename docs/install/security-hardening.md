# Securing tn3270proxy (beyond the quick start)

The Docker quick start optimizes for getting running in 30 seconds. It makes several
deliberate trade-offs you should reverse before any real deployment.

## 1. Replace the generated credentials

The quick start creates an `ADMIN` account and two sample users (`OPERATOR`,
`GUEST`) with generated passwords recorded in `SETUP-DEFAULTS.TXT`.

- Log in as ADMIN, create your own admin account, and delete the `ADMIN` and
  sample accounts via the admin UI (menu `A`).
- Delete `SETUP-DEFAULTS.TXT`.

## 2. Use a real TLS certificate

The quick start generates a self-signed cert for `localhost` (`data/tls/`). Replace
`tls/cert.pem` / `tls/key.pem` with a CA-issued certificate for your real hostname,
or point `listeners.tls.cert`/`key` in `data/tn3270proxy.json` at managed paths.

## 3. Move the MFA master key out of the data directory

The quick start stores the AES-256 MFA key at `data/mfa.key`, next to the database
it encrypts. For production, supply the key out-of-band instead:

- Set `TN3270PROXY_MFA_KEY` (base64 of 32 bytes) in the environment, or
- Point `mfa.key_file` at a path backed by a secret store.

Remove `mfa.key` from the data directory once the key is sourced externally. **Back
up the key** — losing it makes existing MFA enrollments unrecoverable.

## 4. Disable plaintext

The quick start enables plaintext on `:2323` for emulator convenience. For an
exposed gateway, set `listeners.plain.enabled = false` in `tn3270proxy.json` and
serve TLS only. Apply the DoS limits and `trusted_cidrs` described in the main
configuration docs.

## 5. Run the container as non-root

The quick-start image runs as **root** so it can write into a host-owned bind-mounted
data directory, and it writes `SETUP-DEFAULTS.TXT` world-readable so the host user can
open it. For production:

- Run the container as a dedicated non-root UID (e.g. compose `user: "10001:10001"`),
  with a data directory owned by that UID (or use a named volume).
- The MFA key and TLS private key are already `0600`; once you no longer need the host
  user to read `SETUP-DEFAULTS.TXT`, delete it.

## Run without the quick-start wrapper

The `quickstart` subcommand is only the convenience on-ramp. For a controlled
deployment, manage the data dir yourself: `tn3270proxy bootstrap -db <path>` to mint
the first admin, a hand-written `tn3270proxy.json`, and `tn3270proxy serve -config
<path>`.
