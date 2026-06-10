# Logging & external ban scanners

The proxy logs to two concurrent sinks via slog:

- **stderr** — human-readable text, always on (the friendlier console/journald stream).
- **a JSON file** — machine-readable, enabled with `-log-file /path/to/file.json`
  (or `log.file` in the config). Both sinks share the level (`-log-level`,
  default `info`) and carry identical fields.

## Auth-failure lines (fail2ban-friendly)

Every authentication failure — wrong password or wrong TOTP code — emits a
**stable** line you can wire an external scanner (e.g. fail2ban) to. The proxy
does not ship or run fail2ban; it only provides the log surface.

JSON (file sink):

    {"time":"...","level":"WARN","msg":"auth failed","remote":"203.0.113.7:51324","user":"alice","src":"203.0.113.7","trusted":false,"reason":"invalid_credentials"}

Text (stderr sink):

    level=WARN msg="auth failed" remote=203.0.113.7:51324 user=alice src=203.0.113.7 trusted=false reason=invalid_credentials

`src` is the bare client IP (the fail2ban `<HOST>`), `trusted` reflects the
admin trusted-network list (managed in the admin UI under Trusted Networks,
stored in the database), and `reason` is coarse (`invalid_credentials` or
`bad_mfa`) — the on-screen message stays uniform, so the reason never reveals
whether a username exists. The field set is a **stable contract**: filters
depend on it.

> Auth throttling is per-username backoff, not a hard lockout, so repeated
> failures keep emitting these lines; let your scanner's own retry counter
> (fail2ban `maxretry` / `findtime`) decide when to ban. Match `trusted=false`
> so trusted sources are never banned.

## Example fail2ban filter (adapt to your deployment)

Point fail2ban at the JSON log file:

    # /etc/fail2ban/filter.d/tn3270proxy.conf
    [Definition]
    failregex = "msg":"auth failed".*"src":"<HOST>".*"trusted":false
    ignoreregex =

Or, tailing the stderr/journald text stream:

    failregex = msg="auth failed" .*\bsrc=<HOST>\b.*\btrusted=false\b

with a jail like:

    # /etc/fail2ban/jail.d/tn3270proxy.conf
    [tn3270proxy]
    enabled  = true
    filter   = tn3270proxy
    logpath  = /var/log/tn3270proxy/auth.json
    maxretry = 5
    findtime = 10m
    bantime  = 1h
