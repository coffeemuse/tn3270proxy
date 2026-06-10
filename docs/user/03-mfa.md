# Multi-factor authentication

If your account uses MFA, you'll be asked for a 6-digit code from an
authenticator app (any TOTP app — Google Authenticator, Microsoft
Authenticator, FreeOTP, 1Password, …) after your password.

## First-time enrollment

If your administrator requires MFA and you're not enrolled yet, the setup
screen appears right after your first successful login:

```
                       MFA ENROLLMENT - SECURITY KEY SETUP
   Multi-factor authentication is now required for your account.

   Enter the key below into your authenticator app (any TOTP app),
   then type the current 6-digit code to confirm enrollment.

      Issuer:   TN3270PROXY
      Account:  USER2
      Key:      P3KH B6WK IFY2 7PVZ

      Confirmation code:

   Enter=Confirm   PF3=Cancel
```

In your authenticator app choose **manual entry** (there is no QR code on a
3270 terminal) and type the **Key** exactly, ignoring the spaces. The app
starts producing 6-digit codes; type the current one into **Confirmation
code** and press Enter.

- A wrong code shows `Code incorrect - check the key and try again` — the
  usual causes are a typo in the key or a device clock that's off.
- **PF3 cancels** and returns to the login screen — but if MFA is required
  for your account you'll be back here at your next login, with a **new
  key** (each enrollment attempt generates a fresh one, so delete stale
  entries from your app).

You can also enroll voluntarily, before any requirement, from
[User Settings](06-user-settings.md).

## Every login after that

Once enrolled, each login asks for the current code:

```
                                 MFA VERIFICATION
   Enter the current 6-digit code from your authenticator app.

   Code:

   Enter=Verify   PF3=Cancel
```

A wrong or reused code shows `Code incorrect - try again`. Codes are
one-time: a code that just worked won't work again — wait for the app to
show the next one. Like passwords, repeated failures are slowed down
server-side.

## Lost your authenticator?

You can't recover it yourself — contact your administrator, who can clear
your enrollment so you can re-enroll on a new device
([Troubleshooting](09-troubleshooting.md)).
