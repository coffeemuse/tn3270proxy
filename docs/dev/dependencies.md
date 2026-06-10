# Dependencies & supply chain

How tn3270proxy manages third-party code and scans for vulnerabilities. This is
maintainer-facing (for operator deployment hardening, see [`../install/security-hardening.md`](../install/security-hardening.md)).

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

- `go.mod` keeps `go 1.25.0` as the **language floor** and a `toolchain go1.25.<patch>`
  directive selects the **build toolchain**. With `GOTOOLCHAIN=auto` (and
  `actions/setup-go` reading `go-version-file: go.mod`), CI and local builds download
  and use that patched toolchain automatically.
- To clear a fresh batch of stdlib advisories, bump the `toolchain` directive to the
  patch release named in govulncheck's "Fixed in" lines, rebuild, and confirm a clean
  scan. (2026-06-09: bumped to `go1.25.11`, which cleared 17 stdlib advisories.)

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
