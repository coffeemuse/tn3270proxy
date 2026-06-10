# Building from source

## Prerequisites

- **Go 1.25 or newer.** The Go toolchain is the only build dependency.
  (`go.mod` may select a newer patch toolchain; recent Go versions download it
  automatically.)
- **No cgo, no C compiler.** SQLite support is pure Go (`modernc.org/sqlite`),
  so cross-compiling and static binaries work out of the box.

## Build

    go build -o bin/tn3270proxy ./cmd/tn3270proxy

For a release-style build with version stamping (replaces the `dev` default):

    go build -ldflags "-X main.version=vX.Y.Z" -o bin/tn3270proxy ./cmd/tn3270proxy

Print the resolved version:

    ./bin/tn3270proxy version

Plain `go build` without ldflags still reports a useful version — a short VCS
commit hash from `runtime/debug.ReadBuildInfo`, with a `-dirty` suffix when the
working tree has uncommitted changes.

## Optional: the demo backend

The repo also ships `dummy3270`, a tiny standalone TN3270 server useful as a
bridge target while you evaluate the gateway (it's what the Docker quick
start's DEMO menu entry points at):

    go build -o bin/dummy3270 ./cmd/dummy3270
    ./bin/dummy3270 -listen :3300

It has no TLS, no auth, and no state — a test fixture, not a service.

## Next steps

Continue with [First run: bootstrap or seed](04-first-run.md).
