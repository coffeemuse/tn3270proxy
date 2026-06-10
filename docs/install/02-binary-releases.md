# Installing from a binary release

Every tagged release (`vX.Y.Z`) publishes prebuilt, statically-linked binaries
to the project's GitHub Releases page. No Go toolchain required.

## Supported platforms

| OS | Architectures |
|---|---|
| Linux | amd64, arm64 |
| macOS (darwin) | amd64, arm64 (Apple Silicon) |

Each release attaches one archive per platform, named:

    tn3270proxy_<X.Y.Z>_<os>_<arch>.tar.gz

(e.g. `tn3270proxy_X.Y.Z_linux_amd64.tar.gz`), plus a `SHA256SUMS` file
covering all of them. The archive contains the `tn3270proxy` binary,
`LICENSE.md`, `README.md`, and `tn3270proxy.example.json` (a ready-to-copy
starter config).

> Multi-arch **Docker images** for the same releases are published to
> `ghcr.io/coffeemuse/tn3270proxy` — if you'd rather run a container, see the
> [Quick Start (Docker)](01-quickstart.md).

## Download and verify

Download the archive for your platform **and** the `SHA256SUMS` file from the
same release page, then verify the archive before unpacking:

    # Linux
    sha256sum -c SHA256SUMS --ignore-missing

    # macOS
    shasum -a 256 -c SHA256SUMS --ignore-missing

You should see an `OK` line for the file you downloaded. (`--ignore-missing`
silences complaints about the platform archives you didn't download.)

## Install

    tar -xzf tn3270proxy_<X.Y.Z>_<os>_<arch>.tar.gz
    sudo install -m 0755 tn3270proxy /usr/local/bin/

Check it runs and reports the release version:

    tn3270proxy version

Release binaries are version-stamped at build time, so this prints the tag
(e.g. `vX.Y.Z`) — a quick sanity check that you're running what you think you
are.

## Next steps

Continue with [First run: bootstrap or seed](04-first-run.md).
