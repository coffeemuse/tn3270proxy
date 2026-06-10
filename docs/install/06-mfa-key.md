# The MFA master key

TOTP secrets are stored AES-256-GCM encrypted in the database. The key that
encrypts them — the **MFA master key** — is infrastructure-level configuration:
it is supplied to the process at startup and is **never** stored in the
database or editable from the admin UI.

If you don't plan to offer MFA you can skip this page: with no key configured
and no users enrolled, the gateway runs with MFA disabled.

## Generating a key

The key is 32 random bytes, base64-encoded:

    openssl rand -base64 32

Treat it like a private key: never commit it, never log it, and **back it up**
— without it, existing MFA enrollments are unrecoverable.

## Supplying the key

Three sources, in order of precedence:

1. **Environment:** `TN3270PROXY_MFA_KEY` (the base64 string). Wins over the
   config file.
2. **Config file:** `mfa.key` — the base64 string inline in `tn3270proxy.json`.
3. **Config file:** `mfa.key_file` — path to a file containing the base64
   string (use this to source the key from a secret store or a root-owned
   file). `mfa.key` wins if both are set.

```json
{
  "mfa": { "key_file": "/etc/tn3270proxy/mfa.key" }
}
```

## Fail-closed startup checks

`serve` refuses to start — by design — when the key situation is unsafe:

- **Users are enrolled but no key is configured.** Their secrets would be
  undecryptable; configure the key and restart.
- **The configured key cannot decrypt the key-check sentinel** written on the
  first keyed startup. This means the key is wrong or was rotated. Restore the
  correct key, or accept the loss and reset (below).

A correct key on a fresh database simply writes the sentinel and starts.

## Lost or rotated key: break-glass reset

There is no in-place key rotation. If the key is lost, the encrypted secrets
are lost with it; the recovery path wipes every enrollment so users can
re-enroll under the new key:

    TN3270PROXY_MFA_KEY=<new key> tn3270proxy mfa reset-all -db proxy.db

This clears all MFA enrollments and rewrites the sentinel for the supplied
key. Every user enrolls again at their next login (if required) or via User
Settings. The command reads the key from the same sources as `serve`.

## Next steps

Continue with [Running as a service](07-running-as-a-service.md).
