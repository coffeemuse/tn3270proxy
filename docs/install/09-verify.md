# Verifying the install

A working install is verified end-to-end in about a minute: right binary,
reachable listener, working login.

## 1. The binary

    tn3270proxy version

Release builds print their tag (`vX.Y.Z`); source builds print a commit hash.

## 2. The listener

Connect with any TN3270 emulator — `c3270` shown here:

    c3270 <host>:2323        # plaintext listener
    c3270 L:<host>:2324      # TLS listener (c3270's L: prefix = TLS)

With the quick start's self-signed certificate, expect your emulator to prompt
about an untrusted certificate on the TLS port (c3270 needs
`-noverifycert` for self-signed certs).

You should see the **login screen**: a status header on the top rows (title,
date/time, system ID), branding art in the body, and `User ID` / `Password`
input fields near the bottom with the cursor on `User ID`.

## 3. The login

Log in with your admin account (from `bootstrap`, or `SETUP-DEFAULTS.TXT` on a
quick-start install). After any MOTD page (press **ENTER** to page through),
you land on the **service menu**: numbered services on the left, a status
block (user, date/time, terminal, system, release) on the right. An admin
account also sees the `A  Admin` entry.

If a service is configured (the quick start seeds a `DEMO` entry), select it
to verify bridging, then press **PA3** to come back to the menu. **PF3** at
the menu logs off.

## Troubleshooting

- **Connection refused / timeout** — listener address or firewall. Check the
  `listeners` block in your config and the gateway's startup log line for
  each listener.
- **Blank screen or immediate disconnect** — usually a plain client pointed
  at the TLS port or vice versa. Match the port to the connection type.
- **TLS handshake fails** — self-signed cert and a verifying client; tell the
  emulator to accept it (or install a real certificate — see
  [Securing tn3270proxy](08-security-hardening.md)).
- **Login rejected** — the message is deliberately uniform (no hint whether
  the user exists). Usernames are case-insensitive; passwords are not. On a
  fresh manual install, did you run `bootstrap`?
- **Gateway refuses to start with an MFA error** — see
  [The MFA master key](06-mfa-key.md): enrolled users with no key, or a
  wrong/rotated key.

That's a verified install. Next: the [Administration guide](../admin/README.md)
for users, groups, services, and day-to-day operation.
