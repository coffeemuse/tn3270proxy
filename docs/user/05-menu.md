# The service menu

The menu is home base — every service you're allowed to reach, numbered:

```
                               TN3270 GATEWAY MENU             ITEMS 1 TO 3 OF 3
   Option ===>

   Select a service and press ENTER:
   1   CICS       CICS test region                           User ID. : USER1
   2   DEMO       Demonstration system                       Date . . : 26.161
   3   TSO        Production TSO                             Time . . : 06:02
                                                             Terminal : 3279-2
                                                             System ID: PROXY
                                                             Release. : 8e944f2




   0              User Settings

   PF3=Logoff   PF7=PgUp  PF8=PgDn   (PA3 returns here from a session)
```

Each row is a number, the service's short name, and its description. The
block on the right shows who and where you are: your user ID, the date
(YY.DDD Julian) and time, your terminal type, and the gateway's system ID
and release.

Type a number at `Option ===>` and press **Enter** to connect to that
service ([Using a service](08-using-a-service.md)). Anything that isn't on
the menu shows `Invalid selection:` and the menu again.

Two special entries sit at the bottom:

- **`0  User Settings`** — your self-service screen
  ([User Settings](06-user-settings.md)). On some accounts (typically
  shared ones) the administrator disables self-service, and the `0` row
  doesn't appear.
- **`A  Administration`** — only if you're an administrator.

If you have more services than fit on one screen, **PF7/PF8** page up and
down; the `ITEMS x TO y OF z` indicator in the corner shows where you are,
and the numbers stay stable across pages (item 17 is `17` on every page).

**PF3 here is logoff** — back to the login screen. You only see services
your groups grant; if something you need is missing, that's an access
request to your administrator, not an error.
