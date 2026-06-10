# Contributing

TN3270Proxy was built to satisfy a personal need and released in the hope that
it's useful to others. It's maintained as a **personal, best-effort project**,
developed with a spec-driven, test-first workflow. Contributions are welcome
within that spirit — please read this first so we don't waste each other's time.

## Before you start

- **Open an issue first** for anything non-trivial (a bug you intend to fix, or a
  feature). It lets us agree on scope before you write code. For features, describe
  the *problem* you're solving, not just a proposed solution.
- **Security issues do not go in public issues** — see [SECURITY.md](SECURITY.md).
- By contributing, you agree your contributions are licensed under the project's
  **GPL-3.0** license (see [LICENSE.md](LICENSE.md)).

## Development setup

You need Go (the version is pinned in `go.mod`; the toolchain auto-downloads). No cgo.

```bash
go build ./...        # build
go test ./... -race   # tests — the bridge is concurrent, so always use -race
go fmt ./...          # CI fails the build on unformatted code
go vet ./...
```

See [CLAUDE.md](CLAUDE.md) for an architecture tour and the package map, and
[docs/dev/](docs/dev/) for the ISPF style guide and the dependency policy.

## How this project is built

- **Test-driven.** Every package was built test-first; new code should come with tests.
- **Small, single-responsibility files**, one package per concern (`store` owns all
  SQL, `screens` is pure rendering, `auth` never touches the network, …).
- **Conventional-ish commit prefixes:** `feat:`, `fix:`, `docs:`, `chore:`, `test:` —
  small and focused. Release notes are generated from these.
- **Never log credentials** — not passwords, TOTP secrets/codes, or the MFA master key.
- **Schema changes go through the migration ledger** (`internal/store/migrate.go`);
  see CLAUDE.md for the rules (forward-only, append-only).
- **Protocol-facing changes** (screens, cursor, Telnet negotiation, PA3/PF3,
  bridging) must be verified against a real 3270 emulator — unit tests don't catch
  cursor or negotiation bugs. An s3270 smoke harness lives under
  `.claude/skills/s3270-smoke-testing/`.

## Pull requests

Before opening a PR, make sure the CI gates pass locally:

- `go fmt ./...` (clean) · `go vet ./...` · `go build ./...`
- `go test ./... -race`
- `govulncheck ./...`

Keep PRs focused and reference the issue they address. The pull-request template
has the full checklist.
