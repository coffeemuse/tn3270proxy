# Logging in

The login screen shows a status header (date, time, system ID, release) at
the top, your site's branding in the middle, and the credential fields at
the bottom:

```
                               TN3270 GATEWAY LOGIN          Date . . : 26.161
                                                             Time . . : 06:02
                                                             System ID: PROXY
                                                             Release. : 8e944f2

                     EEEEE X   X  AAA  M   M PPPP  L     EEEEE
                     E      X X  A   A MM MM P   P L     E
                     EEE     X   AAAAA M M M PPPP  L     EEE
                     E      X X  A   A M   M P     L     E
                     EEEEE X   X A   A M   M P     LLLLL EEEEE

                           E X A M P L E   C O R P
                      Unauthorized access is prohibited.

   User ID . .                 Password . .
   PF3=Disconnect
```

The cursor is already on **User ID** — type it, press **Tab**, type your
password (it doesn't echo), press **Enter**. User IDs are not
case-sensitive; passwords are.

If the credentials are wrong, the screen returns with
`Invalid user ID or password` in red near the top left. The message is
deliberately the same whether the user ID or the password was at fault.
Repeated failures are slowed down server-side, so after several attempts the
response takes progressively longer — that's throttling, not a hang. There
is no lockout; if you've genuinely forgotten the password, ask your
administrator to reset it (see [Troubleshooting](09-troubleshooting.md)).

**PF3** at the login screen disconnects.

After a successful login you'll see, in order: an MFA prompt (if your
account uses [MFA](03-mfa.md)), the [message of the day](04-motd.md) (if
your site sets one), and then the [service menu](05-menu.md).
