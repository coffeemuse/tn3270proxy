# Dependencies & supply chain

How tn3270proxy manages third-party code and scans for vulnerabilities. This is
maintainer-facing (for operator deployment hardening, see [`../install/08-security-hardening.md`](../install/08-security-hardening.md)).

## Vulnerability scanning (govulncheck)

CI runs `govulncheck ./...` as a dedicated job (`.github/workflows/ci.yml`) on every
push and pull request. govulncheck is symbol-aware: it reports only advisories whose
vulnerable code is actually reachable from ours, so a green run means no *called*
vulnerability — not merely no vulnerable package present.

The job pins the scanner binary (`govulncheck@v1.3.0`) for reproducibility; the
vulnerability database is fetched fresh from `vuln.go.dev` at runtime, so detection
stays current without bumping the pin.

### Standard-library advisories → bump the toolchain

Most findings to date have been Go standard-library advisories (TLS/x509/net/pem on
the inbound listener, backend dialer, and cert loading). These are cleared by building
with a patched Go toolchain, not by code changes. The mechanism:

- `go.mod` keeps `go 1.25.0` as the **language floor** and a `toolchain go1.26.<patch>`
  directive selects the **build toolchain**. With `GOTOOLCHAIN=auto` (and
  `actions/setup-go` reading `go-version-file: go.mod`), CI and local builds download
  and use that patched toolchain automatically.
- To clear a fresh batch of stdlib advisories, bump the `toolchain` directive to the
  patch release named in govulncheck's "Fixed in" lines, rebuild, and confirm a clean
  scan. (2026-06-09: bumped to `go1.25.11`, which cleared 17 stdlib advisories.)
- Rebuild the scanner after a toolchain bump. A `govulncheck` binary built by an older
  Go refuses to load packages compiled by a newer one. CI installs it fresh every run,
  so this bites locally only: re-run `go install golang.org/x/vuln/cmd/govulncheck@v1.3.0`.

### Toolchain currency policy

**Track the current Go major release.** Go supports a major release only until two
more ship, so the second-newest major loses patch support the day the next one lands.
Sitting on an unsupported major means the next stdlib advisory has *no* toolchain bump
that clears it, and the govulncheck gate fails with no fix available.

The rule: when a new Go major ships, bump the `toolchain` directive to the current
major within a release cycle. Three files carry a Go version and must move together:

| File | Line | What it pins |
| --- | --- | --- |
| `go.mod` | `toolchain go1.N.P` | Build toolchain for CI, releases, and local builds |
| `Dockerfile` | `FROM golang:1.N AS build` | Container build stage |
| `docs/install/03-from-source.md` | "Go 1.N or newer" | Language floor for source builders |

The `go` line (language floor) is a **separate decision** and moves in its own PR. It
sets the minimum Go a source builder needs, and it selects the GODEBUG defaults, so it
can change runtime behavior. Bumping it to `go 1.26.0`, for example, enables the
`SecP256r1MLKEM768` and `SecP384r1MLKEM1024` post-quantum TLS key exchanges by default
(`tlssecpmlkem`). No code sets `Config.CurvePreferences`, so that would change the
handshake on both the inbound listener and the backend dialer — smoke-test against a
real backend before taking it.

Dependabot does not bump the `toolchain` directive. These bumps are manual.

- 2026-08-19: bumped to `go1.26.6`. Go 1.25 goes end-of-life when Go 1.27 ships, and
  1.27 was already at rc3. No behavior change: GODEBUG defaults follow the `go` line,
  which stayed at `go 1.25.0`.

## Dependency decision records

### go3270 — `github.com/racingmars/go3270` (pinned, accepted pre-1.0)

**Decision (2026-06-09): ACCEPT, pinned at v0.9.13.**

go3270 is on the protocol-critical path — it renders every 3270 screen and drives
Telnet negotiation. It is a pre-1.0 dependency, which the 1.0-readiness gate (#95)
flagged for a conscious decision.

Rationale for accepting rather than vendoring or forking:

- **Already pinned.** Go modules lock the exact version and content hash in `go.sum`;
  the build cannot silently float to a different go3270.
- **No known vulnerabilities.** govulncheck reports zero findings against go3270.
- **Pre-1.0 API churn is moot while pinned.** The pre-1.0 risk is unstable APIs across
  upgrades, but we upgrade deliberately, not automatically.
- **It is core and we do not control upstream's release cadence.** Versioning to 1.0 is
  upstream's call on their timeline; gating *our* 1.0 on *their* 1.0 is not viable for a
  library this central.

Residual risk: single-maintainer abandonment. **Contingency** if upstream goes dark or
ships a breaking change we can't take: vendor the dependency (`go mod vendor`) or fork
under a project-owned repo with a `replace` directive. Re-evaluate this record if go3270
tags a 1.0.
