# Troubleshooting

**`Invalid user ID or password`, but I'm sure it's right.** Remember the
password is case-sensitive (the user ID isn't). After several failures,
responses get deliberately slower — wait a moment between attempts. If it
still fails, ask your administrator to reset the password; there is no
self-service reset when you can't log in.

**MFA code keeps failing.** Check the device clock — TOTP codes depend on
accurate time, and a phone a minute off produces nothing but wrong codes.
Codes are also one-time: wait for a fresh one rather than re-entering the
last. During enrollment, re-check the key for typos.

**Lost or replaced your phone.** An administrator must clear your MFA
enrollment; then you either re-enroll at the next login (if MFA is required)
or via [User Settings](06-user-settings.md).

**Nothing appears when I connect / the connection drops immediately.**
Usually a TLS mismatch: a plain connection to the TLS port (or the reverse).
Confirm with your administrator which port is which and whether your
emulator's secure-connection setting should be on.

**Certificate warning when connecting.** The gateway's certificate isn't
trusted by your machine. At some sites that's expected (self-signed) — ask
your administrator before accepting.

**I was sent back to the login screen without doing anything.** Idle
timeout — you were logged off after inactivity. Log in again.

**A service I need isn't on my menu.** Menus are per-account: you only see
services your groups grant. Ask your administrator for access.

**`Could not connect to <NAME>` when selecting a service.** The backend
system is unreachable — it, not the gateway, is likely down. Report it.

**My keyboard's first keystrokes after connecting get lost.** Some emulators
deliver type-ahead before the screen settles. Wait for the login screen to
paint and the cursor to land on User ID before typing.
