# Connection limits & idle timeouts

The `limits` section of `tn3270proxy.json` (deployment configuration, not
the admin UI) controls how the gateway defends its connection slots:

```json
{
  "limits": {
    "pre_auth_idle": "2m",
    "pre_auth_max":  "5m",
    "idle":          "30m",
    "max_conns":     512,
    "max_per_ip":    16,
    "bridge_idle":   "disconnect"
  }
}
```

Durations are Go strings (`90s`, `2m`, `1h`). Each key can also be set with a
matching `serve` flag (`-pre-auth-idle`, `-idle`, etc. — see the
[CLI reference](14-cli.md)).

## The three idle regimes

The gateway switches timeout regimes as a session moves through its
lifecycle:

- **Before login** — two clocks run at once: a sliding idle window
  (`pre_auth_idle`, default 2m) that re-arms on any traffic, and an
  **absolute ceiling** (`pre_auth_max`, default 5m) that does **not** slide.
  The ceiling is the anti-trickle defense: a client feeding one byte every
  90 seconds can defeat an idle timer, but not a hard deadline. Either
  expiring disconnects the connection. Connections from
  [Trusted Networks](09-trusted-networks.md) skip both.
- **At the menu / admin screens** — one sliding window (`idle`, default
  30m). Expiry **logs the user out to the login screen** rather than
  disconnecting (audited as `logout` / `idle logout`), so a parked terminal
  ends up safe but still connected.
- **During a bridged session** — by default the `idle` window still runs and
  expiry disconnects. Set `"bridge_idle": "exempt"` if your backends manage
  their own idle policy (or sessions legitimately sit quiet for hours) and
  you'd rather the gateway not cut them.

## Connection caps

- **`max_conns`** (default 512) — global cap, claimed **before** accepting:
  over-cap connections simply wait in the kernel backlog rather than
  consuming gateway resources. Everyone counts, trusted or not.
- **`max_per_ip`** (default 16; `0` disables) — per-source-IP cap, enforced
  after accept by closing the excess connection. Trusted networks are
  exempt — set that up instead of raising the cap globally if one known
  gateway IP (e.g. a web 3270 front-end) needs more.

Cap events are logged, so a flood shows up in the
[logs](13-logging.md) even though over-cap clients never reach a screen.
