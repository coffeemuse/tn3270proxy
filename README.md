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

## Status

MVP. Not yet implemented (see spec section 9): TLS listener, backend TLS, admin UI,
TN3270E, audit logging, session multiplexing.

## Test

    go test ./...
