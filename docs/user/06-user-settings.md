# User Settings (menu option 0)

Option `0` on the service menu opens your self-service screen:

```
                           TN3270 GATEWAY USER SETTINGS
   Option ===>

   User: USER1

   1   Password   Change your sign-on password
   2   MFA        Enroll in multi-factor authentication

   PF3=Service Menu
```

The options adapt to your account's state:

- **`1 Password`** — always present.
- **`2 MFA`** — `Enroll` if you have no authenticator set up, or
  `Re-enroll your authenticator (replaces the key)` if you do (use this when
  you've moved to a new phone).
- **`3 MFA Disable`** — only when you're enrolled *and* your administrator
  doesn't require MFA for your account. If MFA is required, only an
  administrator can remove it.

If MFA is not enabled on the gateway at all, only the password option shows.
PF3 returns to the service menu.

## Changing your password

Option `1` asks for your **current password**, the **new password**, and the
new one again. The new password must actually differ
(`New password must differ from current`). Wrong current-password attempts
are throttled like login failures.

## Managing your MFA

Every MFA action (enroll, re-enroll, disable) first asks you to **confirm
your password** — a one-field screen titled `TN3270 GATEWAY: CONFIRM
PASSWORD`. That's deliberate: a walked-away-from terminal shouldn't be
enough to change security settings.

Enrollment and re-enrollment then follow the same key-and-confirm flow as
[first-time enrollment](03-mfa.md). Re-enrolling **replaces** the old key —
the codes on your old device stop working the moment the new enrollment is
confirmed.

## If option 0 isn't on your menu

Your account has self-service disabled by the administrator (common for
shared accounts). Password changes and MFA changes for such accounts go
through your administrator.
