# CI/CD Release Pipeline — Design

**Issue:** [#67](https://github.com/CoffeeMuse/tn3270proxy/issues/67) — CI/CD: tagged-release binaries (zipped + attached) and Docker images with sample docker-compose
**Date:** 2026-06-06
**Status:** Approved (brainstorm) — pending implementation plan

## Summary

Establish the project's first build/test and release automation. Two independent
deliverables, shipped as **two separate PRs**:

1. **Test CI** — a `ci.yml` workflow that builds and tests every PR and push to
   `main`, surfaced via a README status badge. The repo currently has **no
   build/test CI** (only the Claude bot workflows).
2. **Release pipeline** — on a pushed `vX.Y.Z` tag: cross-platform binaries
   (GoReleaser) attached to a GitHub Release, and native multi-arch Docker images
   published to GHCR, plus a sample `docker-compose.yml` for operators.

The release pipeline lands on top of the test gate, not before it.

## Background / current state (verified)

- Module `github.com/CoffeeMuse/tn3270proxy`; single entrypoint `cmd/tn3270proxy`.
- Pure Go, **no cgo** (`modernc.org/sqlite`), `go 1.25.0` directive — cross-compile
  is clean (`GOOS`/`GOARCH`, `CGO_ENABLED=0`).
- Version stamping already wired: `-ldflags "-X main.version=vX.Y.Z"`, resolved by
  `internal/version` with a VCS-hash fallback, exposed via the `version` subcommand.
- `LICENSE.md`, `README.md`, and `tn3270proxy.example.json` exist and are the files
  to bundle into release archives.
- **No git tags yet** — this pipeline drives the first formal release.
- Only `.github/workflows/claude.yml` and `claude-code-review.yml` exist. No
  `Dockerfile`, `.dockerignore`, `docker-compose.yml`, or `examples/`.

## Decisions (locked during brainstorm)

| Decision | Choice | Rationale |
|---|---|---|
| Binary tooling | **GoReleaser** | Project is GoReleaser's happy path (pure Go, single binary, ldflags already wired). Collapses matrix build + archive + checksums + release notes into one config. CI-only dependency — runtime artifact stays dependency-free. |
| Scoping | **Test CI first, separate PR**, then release pipeline | A release pipeline without a green-tests gate is the risky part. Each PR is independently verifiable. |
| Multi-arch Docker | **Native runners (`ubuntu-24.04` + `ubuntu-24.04-arm`), NO QEMU** | GitHub now offers native arm64 runners. GoReleaser OSS can't do native multi-arch Docker in one job (`--split` is Pro-only), so Docker is decoupled from GoReleaser and built with buildx on a runner matrix. |
| Release notes | **Auto-generated from commits** | Existing conventional-ish commit prefixes (`feat:`/`fix:`/`docs:`) make this clean. No `CHANGELOG.md` to hand-maintain. |
| Branch protection | **Out of scope** | Workflows/files only; repo-settings changes left to the maintainer. |
| Build matrix | **`linux/{amd64,arm64}` + `darwin/{amd64,arm64}`** (no windows) | Windows dropped per maintainer; not a meaningful target for a mainframe gateway. |
| Docker base | **`gcr.io/distroless/static:nonroot`** | Static binary makes it viable; non-root by default; minimal attack surface (no shell/pkg manager) for an internet-facing gateway. |

## Out of scope (YAGNI / deferred)

- Cosign image signing / SBOM / provenance attestation (issue defers to follow-up).
- Branch-protection / required-checks repo settings.
- A hand-maintained `CHANGELOG.md`.
- `windows/amd64` build target.

---

## PR 1 — Test CI

### `.github/workflows/ci.yml`
- **Triggers:** `pull_request` and `push` to `main`.
- **Single job `test`** on `ubuntu-24.04`:
  1. `actions/checkout`
  2. `actions/setup-go` — version derived from `go.mod` (`go-version-file: go.mod`),
     module/build cache enabled.
  3. `go vet ./...` — cheap, catches what tests don't.
  4. `go build ./...`
  5. `go test ./... -race` — the bridge is concurrent; `-race` is mandatory per
     project convention. (Subsumes a plain test run.)

### README badge
Add at the top of `README.md`:

```markdown
[![CI](https://github.com/CoffeeMuse/tn3270proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/CoffeeMuse/tn3270proxy/actions/workflows/ci.yml)
```

Reflects `main`'s build+test health — the always-on signal operators and
contributors check first. (The release workflow only runs on tags, so it is a poor
status badge by comparison.)

### Verification
The PR's own `ci.yml` run going green is the proof.

---

## PR 2 — Release pipeline

Triggered by pushing a tag matching `v*`. Composed of four files plus a workflow.

### 2a. Binaries — `.goreleaser.yaml`

- **`builds`:** one binary from `cmd/tn3270proxy`.
  - `env: [CGO_ENABLED=0]`
  - `ldflags: -s -w -X main.version={{.Version}}`
  - `goos: [linux, darwin]`, `goarch: [amd64, arm64]` (4 targets; no windows).
  - All four cross-compiled from the single amd64 runner — no emulation (Go
    cross-compiles natively for binary builds).
- **`archives`:** format `tar.gz`; name template
  `tn3270proxy_{{ .Version }}_{{ .Os }}_{{ .Arch }}`; `files:` include `LICENSE.md`,
  `README.md`, `tn3270proxy.example.json`.
- **`checksum`:** `name_template: SHA256SUMS`, algorithm sha256.
- **`changelog`:** `use: github`, grouped by conventional prefix
  (`feat` → Features, `fix` → Bug Fixes, `docs` → Documentation, others → Others).
- **`release`:** `prerelease: auto` — tags with a suffix (e.g. `-rc1`) are marked
  GitHub pre-releases automatically.

### 2b. Docker — `Dockerfile` + `.dockerignore`

**`Dockerfile`** (multi-stage, self-contained so `docker build .` works for anyone):
- **Stage `build`:** `golang:1.25`. `ARG VERSION`. `COPY` source, `go build` with
  `CGO_ENABLED=0` and `-ldflags "-s -w -X main.version=${VERSION}"` →
  `/out/tn3270proxy`.
- **Stage final:** `gcr.io/distroless/static:nonroot`. `COPY --from=build` the
  binary. `USER nonroot`. `EXPOSE 2323` (document `2324` is the config-file TLS
  listener). `ENTRYPOINT ["/tn3270proxy"]`, default `CMD ["serve"]`.

**`.dockerignore`:** keep the build context to source + `go.mod`/`go.sum`. Exclude
`.git`, `.claude`, `docs`, `bin`, `*.db`, and the example configs (`*.example.json`
ship in the GoReleaser archives, not in the image).

### 2c. Release workflow — `.github/workflows/release.yml`

- **Triggers:** `push: tags: ['v*']` and `workflow_dispatch` (dry-run).
- **Permissions:** `contents: write` (release assets), `packages: write` (GHCR).
- **Job `binaries`** (`ubuntu-24.04`):
  - `actions/checkout` (full history — `fetch-depth: 0` for changelog), `setup-go`.
  - `goreleaser/goreleaser-action`:
    - On a real tag: `release --clean`.
    - On `workflow_dispatch`: `release --snapshot --clean` (build + archive +
      checksum, **no publish**) — the dry-run path.
  - `GITHUB_TOKEN` provided.
- **Job `docker`** (matrix; only on real tags, skipped for `workflow_dispatch`):
  - Matrix: `{ runner: ubuntu-24.04, platform: linux/amd64 }`,
    `{ runner: ubuntu-24.04-arm, platform: linux/arm64 }`.
  - `docker/setup-buildx-action`, `docker/login-action` (GHCR, `GITHUB_TOKEN`).
  - `docker/build-push-action`: build the single matrix platform **natively** (no
    QEMU), push **by digest** (`outputs: type=image,push-by-digest=true,name-canonical`),
    pass `VERSION` build-arg from the tag (`${{ github.ref_name }}`).
  - Export the digest as a workflow artifact for the merge job.
- **Job `docker-manifest`** (`needs: [docker]`):
  - Download digests, `docker/login-action`.
  - `docker/metadata-action` computes tags on `ghcr.io/coffeemuse/tn3270proxy`:
    `type=semver` patterns `{{version}}` (X.Y.Z), `{{major}}.{{minor}}` (X.Y),
    `{{major}}` (X), and `latest` with `flavor: latest=auto` so prereleases are
    excluded from `latest`.
  - `docker buildx imagetools create` fuses the per-arch digests into the
    multi-arch tags.

### 2d. Sample compose — `examples/docker-compose.yml`

A documented compose file that:
- Runs `ghcr.io/coffeemuse/tn3270proxy:latest` (commented: pin a version in prod).
- Mounts a **named volume** for `proxy.db`.
- Supplies `TN3270PROXY_MFA_KEY` via env, with a comment to generate it
  (`openssl rand -base64 32`); notes the mounted-key-file alternative.
- Bind-mounts a config file and/or TLS certs; maps the listener port(s).
- Header comments cover: the one-shot `bootstrap` first-admin invocation, and the
  `tn3270proxy.json` `:2324` TLS-listener collision gotcha.
- Cross-referenced from README / install docs (ties to #66).

### Verification
- **`workflow_dispatch` snapshot** exercises the full binary build/archive/checksum
  path without publishing.
- **`docker build .`** locally smoke-checks the Dockerfile.
- The complete tag → GitHub Release → GHCR path is only fully exercised by pushing a
  real low-version tag (e.g. `v0.1.0`). This is called out as the **final manual
  validation step** — CI cannot fully self-prove the publish path.

---

## File manifest

**PR 1:**
- `.github/workflows/ci.yml` (new)
- `README.md` (badge)

**PR 2:**
- `.goreleaser.yaml` (new)
- `Dockerfile` (new)
- `.dockerignore` (new)
- `.github/workflows/release.yml` (new)
- `examples/docker-compose.yml` (new)
- `README.md` / install docs (cross-reference compose + images)
