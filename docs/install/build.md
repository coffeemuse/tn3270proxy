# Building from source & release artifacts

## Build

    go build -o bin/tn3270proxy ./cmd/tn3270proxy

For a release build with version stamping (replaces the `dev` default):

    go build -ldflags "-X main.version=v1.2.3" -o bin/tn3270proxy ./cmd/tn3270proxy

Print the resolved version:

    ./bin/tn3270proxy version

Plain `go build` without ldflags still reports a useful version — a short VCS commit
hash from `runtime/debug.ReadBuildInfo`, with a `-dirty` suffix when the working tree
has uncommitted changes.

## Container images & releases

Tagged releases (`vX.Y.Z`) publish cross-platform binaries (with `SHA256SUMS`) to the
GitHub Releases page, and multi-arch Docker images to
`ghcr.io/coffeemuse/tn3270proxy` (tags: `X.Y.Z`, `X.Y`, `X`, and `latest` for stable
releases). See [`examples/docker-compose.yml`](../../examples/docker-compose.yml) for a
ready-to-run deployment (persistent DB volume, MFA key, config/TLS mounts, and the
first-admin bootstrap step).
