# TN3270Proxy

A TN3270 gateway: presents itself as a TN3270 server, authenticates users,
shows a group-filtered menu of internal TN3270 services, and bridges the user
to the selected service. See `docs/superpowers/specs/` for the design.

## Build

    go build -o bin/tn3270proxy ./cmd/tn3270proxy

## Seed users / groups / services

    ./bin/tn3270proxy seed -db proxy.db -file seed.example.json

See `seed.example.json` for the format.

## Run

    ./bin/tn3270proxy serve -db proxy.db -listen :2323

Connect with any TN3270 emulator (e.g. `c3270 host:2323`). Press **PA3** during
a bridged session to return to the menu; **PF3** at the menu disconnects.

### Configuration file

Richer setup (e.g. TLS) uses a JSON config file. By default `serve` looks for
`tn3270proxy.json` in the working directory; pass `-config <path>` to choose
another. Flags (`-listen`, `-db`) override file values, which override built-in
defaults. `tn3270proxy.example.json` is a ready-to-copy starter (TLS off by
default). For example, to enable both listeners at once:

    {
      "db": "tn3270proxy.db",
      "listeners": {
        "plain": { "enabled": true,  "addr": ":2323" },
        "tls":   { "enabled": true,  "addr": ":3270",
                   "cert": "server.crt", "key": "server.key" }
      }
    }

Both listeners are independent: enable either or both. TLS is terminated
immediately on connect (not STARTTLS), as TN3270-over-TLS clients expect.
Unknown keys in the config file are rejected, so a typo fails loudly rather
than silently disabling a listener.

Generate a self-signed cert/key for local testing (gitignored):

    openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
      -nodes -keyout server.key -out server.crt -days 365 \
      -subj "/CN=localhost" -addext "subjectAltName=IP:127.0.0.1,DNS:localhost"

## Status

MVP + TLS inbound listener. Not yet implemented (see spec section 9 / ROADMAP):
backend TLS, admin UI, TN3270E, audit logging, session multiplexing.

## Test

    go test ./...
