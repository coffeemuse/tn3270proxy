# Running as a service

How to run the gateway under a process manager on a plain host. (If you deploy
with Docker, `examples/docker-compose.yml` plus a restart policy covers this —
see the [Quick Start](01-quickstart.md).)

## Layout and permissions

A deployment is five files. A conventional layout:

    /usr/local/bin/tn3270proxy            the binary (root-owned, 0755)
    /etc/tn3270proxy/tn3270proxy.json     config (read-only to the service user)
    /etc/tn3270proxy/mfa.key              MFA master key, if used (0600)
    /etc/tn3270proxy/tls/                 TLS cert + key (key 0600)
    /var/lib/tn3270proxy/proxy.db         the SQLite database

Run as a **dedicated non-root user**. The database's *directory* must be
writable by that user — SQLite working files and the automatic pre-upgrade
backups (`proxy.db.pre-migrate-*.bak`) are written next to the database. Keep
`proxy.db`, the MFA key, and the TLS private key readable only by the service
user (`0600`).

Always pass `-config` explicitly. Without it, `serve` looks for
`tn3270proxy.json` in the *current working directory*, which under a process
manager is rarely what you expect.

## Example systemd unit

A starting point — adjust paths, then `systemctl enable --now tn3270proxy`:

    # /etc/systemd/system/tn3270proxy.service
    [Unit]
    Description=TN3270 gateway
    After=network-online.target
    Wants=network-online.target

    [Service]
    User=tn3270proxy
    Group=tn3270proxy
    ExecStart=/usr/local/bin/tn3270proxy serve -config /etc/tn3270proxy/tn3270proxy.json
    Restart=on-failure

    # MFA master key via environment (alternative: mfa.key_file in the config)
    # The file contains: TN3270PROXY_MFA_KEY=<base64 key>
    EnvironmentFile=-/etc/tn3270proxy/mfa.env

    # Basic sandboxing
    NoNewPrivileges=yes
    ProtectSystem=strict
    ProtectHome=yes
    ReadWritePaths=/var/lib/tn3270proxy

    [Install]
    WantedBy=multi-user.target

The default ports (`:2323`, `:2324`) are unprivileged. If you bind a port
below 1024 (e.g. `:23`), add
`AmbientCapabilities=CAP_NET_BIND_SERVICE` to the unit.

## Logs

The gateway always writes human-readable text to **stderr**, which systemd
captures in the journal (`journalctl -u tn3270proxy`). For a machine-readable
stream (e.g. for fail2ban), add `-log-file /var/log/tn3270proxy/proxy.json` or
set `log.file` in the config — see the
[Administration guide on logging](../admin/logging.md).

## Upgrades

Stop the service, replace the binary, start it again; schema migrations run
automatically with a backup written first. Details and rollback:
[Upgrading & database backups](../admin/operations-upgrades.md).

## Next steps

Continue with [Securing tn3270proxy](08-security-hardening.md).
