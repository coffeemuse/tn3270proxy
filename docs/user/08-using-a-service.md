# Using a service

Type the service's number at the menu and press Enter. The gateway connects
to the backend system and from then on your terminal talks to it directly —
its screens, its keys, its login if it has one:

```
                                    CICSDEMO                           DUMMY3270

           WELCOME TO CICSDEMO

           CICS/TS region CICSDEMO is now available.
           This is a non-working demonstration host.

   Press PA3 to disconnect.
```

(That's the demo backend; yours will be a real TSO, CICS, or other host
screen.)

While bridged, the gateway is invisible except for one key: **PA3 returns
you to the service menu** (see [Keys and timeouts](07-navigation.md)). You
can then pick another service or PF3 to log off.

- **If the backend can't be reached**, the menu returns with
  `Could not connect to <NAME>` — try again or report it.
- **If the backend ends the session** (you logged off it, or it closed the
  connection), you land back on the menu automatically.
