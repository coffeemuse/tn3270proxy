# Keys and timeouts

## PF3 — one step back

PF3 always backs up exactly one level:

    admin sub-screen → admin menu → service menu → login screen → disconnect

So from the service menu, PF3 is **logoff** (back to the login screen), and
a second PF3 disconnects. From User Settings or an MFA prompt, PF3 returns
you to where you came from.

## PA3 — escape from a service

While you're connected to a service, every key you press belongs to that
backend system — except **PA3**, which the gateway reserves: it ends the
backend session and returns you to the service menu. The menu's help line
reminds you: `(PA3 returns here from a session)`.

On the gateway's own screens (login, menu, MOTD, MFA, settings) PA3 does
nothing.

> If your emulator's keyboard doesn't have PA3 mapped, check its keymap —
> in c3270/x3270 it's available as the `PA(3)` action, and most emulators
> let you bind it to a key.

## PF1 — service menu help

At the service menu, **PF1** opens a help screen describing how to use the
menu. Use **PF7/PF8** to page through the help text. Press **PF3** or
**ENTER** to return — you'll land back on the same page of the service menu
you were on.

## If you walk away

- **At the menu or other gateway screens**: after a period of inactivity
  (set by your site) you're **logged off back to the login screen** — your
  session stays connected, but re-authentication is required.
- **At the login screen**: idle connections are dropped after a few minutes.
- **In a service session**: most sites disconnect idle bridged sessions;
  the idle limit is your site's policy. Anything you had open on the backend
  is subject to that backend's own rules.
