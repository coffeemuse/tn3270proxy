# CI/CD Release Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a build/test CI workflow and a tagged-release pipeline (GoReleaser binaries + native multi-arch GHCR Docker images + sample docker-compose) for the TN3270 gateway.

**Architecture:** Two independent PRs. PR 1 is a `ci.yml` build+test gate with a README badge. PR 2 is the release pipeline: GoReleaser builds/archives/publishes the 4 binary targets on a `v*` tag, while Docker images are built **natively** on an amd64 + arm64 runner matrix (no QEMU) and fused into multi-arch GHCR tags via a manifest job; plus a documented `examples/docker-compose.yml`.

**Tech Stack:** GitHub Actions, GoReleaser v2, Docker Buildx, distroless, Go 1.25 (pure Go, `CGO_ENABLED=0`).

**Spec:** `docs/superpowers/specs/2026-06-06-cicd-release-pipeline-design.md`

**Validation tooling note:** `goreleaser`, `actionlint`, and `hadolint` are NOT installed globally. This plan invokes them via `go run <module>@<version>` (no global install, uses the project Go toolchain). `docker` IS installed locally, so the Dockerfile gets a real build smoke test.

---

## PR 1 — Test CI

Branch from `main`: `git switch main && git switch -c ci/test-workflow` (or use the
current worktree branch if executing here — just keep PR 1's commits separate from
PR 2's).

### Task 1: CI build+test workflow

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Create the workflow file**

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

permissions:
  contents: read

jobs:
  test:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - name: Vet
        run: go vet ./...
      - name: Build
        run: go build ./...
      - name: Test (race)
        run: go test ./... -race
```

- [ ] **Step 2: Validate workflow syntax**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/ci.yml`
Expected: no output, exit code 0. (actionlint prints nothing on success.)

- [ ] **Step 3: Sanity-check the commands the workflow runs**

Run: `go vet ./... && go build ./... && go test ./... -race`
Expected: all pass (this is what CI will run; the bridge is concurrent so `-race` is required by project convention).

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add build+test workflow (GH #67)"
```

### Task 2: README CI badge

**Files:**
- Modify: `README.md` (top of file, near the title)

- [ ] **Step 1: Inspect the current README header**

Run: `head -10 README.md`
Expected: see the H1 title line so the badge can be placed directly beneath it.

- [ ] **Step 2: Add the badge under the title**

Insert this line immediately after the H1 title line (blank line above and below):

```markdown
[![CI](https://github.com/CoffeeMuse/tn3270proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/CoffeeMuse/tn3270proxy/actions/workflows/ci.yml)
```

- [ ] **Step 3: Verify placement**

Run: `head -10 README.md`
Expected: badge markdown appears directly under the title line.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: add CI status badge to README (GH #67)"
```

**PR 1 done.** Open a PR; its own `ci.yml` run going green is the acceptance proof. Merge before PR 2 so the badge resolves and `main` has a green-tests gate.

---

## PR 2 — Release pipeline

Branch from `main` (after PR 1 merges): `git switch main && git pull && git switch -c release/pipeline`.

### Task 3: GoReleaser config (binaries)

**Files:**
- Create: `.goreleaser.yaml`

- [ ] **Step 1: Create the GoReleaser config**

Create `.goreleaser.yaml`:

```yaml
version: 2
project_name: tn3270proxy

before:
  hooks:
    - go mod tidy

builds:
  - id: tn3270proxy
    main: ./cmd/tn3270proxy
    binary: tn3270proxy
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w -X main.version={{ .Version }}

archives:
  - id: default
    formats:
      - tar.gz
    name_template: "tn3270proxy_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - LICENSE.md
      - README.md
      - tn3270proxy.example.json

checksum:
  name_template: "SHA256SUMS"
  algorithm: sha256

changelog:
  use: github
  groups:
    - title: Features
      regexp: '^.*?feat(\(.+\))??!?:.+$'
      order: 0
    - title: Bug Fixes
      regexp: '^.*?fix(\(.+\))??!?:.+$'
      order: 1
    - title: Documentation
      regexp: '^.*?docs(\(.+\))??!?:.+$'
      order: 2
    - title: Others
      order: 999

release:
  prerelease: auto
```

- [ ] **Step 2: Validate the config**

Run: `go run github.com/goreleaser/goreleaser/v2@latest check`
Expected: `1 configuration file(s) validated` / `command finished successfully` with no errors. (If it reports a deprecated field, fix it inline and re-run.)

- [ ] **Step 3: Dry-run a full snapshot build (no publish)**

Run: `go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean --skip=docker`
Expected: builds all 4 targets and writes archives + `SHA256SUMS` under `dist/`. Verify:
Run: `ls dist/*.tar.gz dist/SHA256SUMS`
Expected: 4 `.tar.gz` files (linux/darwin × amd64/arm64) and one `SHA256SUMS`.

- [ ] **Step 4: Verify version injection in a built binary**

Run: `tar -xzf dist/tn3270proxy_*_linux_amd64.tar.gz -O tn3270proxy > /tmp/tn3270proxy_rel 2>/dev/null; ls -l dist/`
(Then, on a matching-arch machine, `./tn3270proxy version` would print the snapshot version. On a non-linux host, confirming the archive contains the binary + LICENSE.md + README.md + tn3270proxy.example.json is sufficient:)
Run: `tar -tzf dist/tn3270proxy_*_linux_amd64.tar.gz`
Expected: lists `tn3270proxy`, `LICENSE.md`, `README.md`, `tn3270proxy.example.json`.

- [ ] **Step 5: Clean the dist dir and commit**

```bash
rm -rf dist
git add .goreleaser.yaml
git commit -m "build: add GoReleaser config for release binaries (GH #67)"
```

(`dist/` is GoReleaser's default output and is gitignored by GoReleaser convention; if not already ignored, add `dist/` to `.gitignore` in this commit.)

### Task 4: Dockerfile + .dockerignore

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`

- [ ] **Step 1: Create the Dockerfile**

Create `Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1

FROM golang:1.25 AS build
WORKDIR /src
ARG VERSION=dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/tn3270proxy ./cmd/tn3270proxy

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/tn3270proxy /tn3270proxy
USER nonroot:nonroot
# 2323 = plaintext listener. The config-file TLS listener (tn3270proxy.json) is :2324.
EXPOSE 2323
ENTRYPOINT ["/tn3270proxy"]
CMD ["serve"]
```

- [ ] **Step 2: Create the .dockerignore**

Create `.dockerignore`:

```
.git
.github
.claude
docs
bin
dist
*.db
*.example.json
*.md
!README.md
```

(README.md is re-included because the build context COPY `. .` is fine without docs; keeping README is harmless and lets future image-doc steps use it. The example configs and the heavy `docs/`, `.git`, `.claude`, `dist`, and runtime `*.db` files are excluded to keep the context lean.)

- [ ] **Step 3: Build the image locally (real smoke test — docker is installed)**

Run: `docker build --build-arg VERSION=v0.0.0-smoke -t tn3270proxy:smoke .`
Expected: build succeeds; final stage based on distroless.

- [ ] **Step 4: Verify the binary runs and reports the injected version**

Run: `docker run --rm tn3270proxy:smoke version`
Expected: prints `v0.0.0-smoke` (confirms ENTRYPOINT + ldflags injection).

- [ ] **Step 5: Verify it runs as non-root**

Run: `docker run --rm --entrypoint "" tn3270proxy:smoke /tn3270proxy version` is not needed; instead inspect the user:
Run: `docker inspect tn3270proxy:smoke --format '{{.Config.User}}'`
Expected: `nonroot:nonroot`.

- [ ] **Step 6: Commit**

```bash
git add Dockerfile .dockerignore
git commit -m "build: add multi-stage distroless Dockerfile (GH #67)"
```

### Task 5: Release workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Create the release workflow**

Create `.github/workflows/release.yml`:

```yaml
name: Release

on:
  push:
    tags: ['v*']
  workflow_dispatch:

permissions:
  contents: write
  packages: write

env:
  IMAGE: ghcr.io/coffeemuse/tn3270proxy

jobs:
  binaries:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: '~> v2'
          args: >-
            ${{ github.event_name == 'workflow_dispatch'
                && 'release --snapshot --clean --skip=docker'
                || 'release --clean --skip=docker' }}
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}

  docker:
    if: github.event_name == 'push'
    runs-on: ${{ matrix.runner }}
    strategy:
      fail-fast: false
      matrix:
        include:
          - runner: ubuntu-24.04
            platform: linux/amd64
            arch: amd64
          - runner: ubuntu-24.04-arm
            platform: linux/arm64
            arch: arm64
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-buildx-action@v3
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - id: build
        uses: docker/build-push-action@v6
        with:
          context: .
          platforms: ${{ matrix.platform }}
          build-args: |
            VERSION=${{ github.ref_name }}
          outputs: type=image,name=${{ env.IMAGE }},push-by-digest=true,name-canonical=true,push=true
      - name: Export digest
        run: |
          mkdir -p /tmp/digests
          digest="${{ steps.build.outputs.digest }}"
          touch "/tmp/digests/${digest#sha256:}"
      - uses: actions/upload-artifact@v4
        with:
          name: digests-${{ matrix.arch }}
          path: /tmp/digests/*
          retention-days: 1

  docker-manifest:
    if: github.event_name == 'push'
    needs: [docker]
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/download-artifact@v4
        with:
          path: /tmp/digests
          pattern: digests-*
          merge-multiple: true
      - uses: docker/setup-buildx-action@v3
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - id: meta
        uses: docker/metadata-action@v5
        with:
          images: ${{ env.IMAGE }}
          flavor: latest=auto
          tags: |
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
            type=semver,pattern={{major}}
      - name: Create multi-arch manifest
        working-directory: /tmp/digests
        run: |
          docker buildx imagetools create \
            $(jq -cr '.tags | map("-t " + .) | join(" ")' <<< "$DOCKER_METADATA_OUTPUT_JSON") \
            $(printf '${{ env.IMAGE }}@sha256:%s ' *)
      - name: Inspect result
        run: docker buildx imagetools inspect ${{ env.IMAGE }}:${{ steps.meta.outputs.version }}
```

- [ ] **Step 2: Validate workflow syntax**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/release.yml`
Expected: no output, exit 0. (If it warns about `ubuntu-24.04-arm`, that's a known runner-label false positive on older actionlint — confirm the label is correct and proceed.)

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: add tagged-release workflow (binaries + GHCR multi-arch) (GH #67)"
```

### Task 6: Sample docker-compose

**Files:**
- Create: `examples/docker-compose.yml`

- [ ] **Step 1: Create the compose file**

Create `examples/docker-compose.yml`:

```yaml
# TN3270 gateway — sample docker-compose.
#
# FIRST-RUN BOOTSTRAP (one-shot, creates the first admin):
#   docker compose run --rm tn3270proxy bootstrap -db /data/proxy.db
# Then start the gateway normally with:
#   docker compose up -d
#
# MFA MASTER KEY: generate once and keep it secret/stable:
#   openssl rand -base64 32
# Supply it via TN3270PROXY_MFA_KEY below (or mount a key file and use mfa.key_file
# in the config). The server refuses to start if enrolled users exist but no key is set.
#
# GOTCHA: a tn3270proxy.json in the working dir auto-enables a TLS listener on :2324.
# This sample uses an explicit config mount; map :2324 too if you enable TLS.

services:
  tn3270proxy:
    image: ghcr.io/coffeemuse/tn3270proxy:latest   # pin a version (e.g. :1.0.0) in production
    restart: unless-stopped
    command: ["serve", "-db", "/data/proxy.db", "-listen", ":2323"]
    ports:
      - "2323:2323"      # plaintext TN3270 listener
      # - "2324:2324"    # TLS listener (uncomment if you mount a TLS-enabled config)
    environment:
      # Generate with: openssl rand -base64 32
      TN3270PROXY_MFA_KEY: "${TN3270PROXY_MFA_KEY:?set TN3270PROXY_MFA_KEY in your .env}"
    volumes:
      - proxy-db:/data                              # persistent SQLite DB
      # - ./tn3270proxy.json:/tn3270proxy.json:ro   # optional config (enables :2324 TLS)
      # - ./certs:/certs:ro                          # optional TLS cert/key material

volumes:
  proxy-db:
```

- [ ] **Step 2: Validate the compose file (docker is installed)**

Run: `TN3270PROXY_MFA_KEY=dummy docker compose -f examples/docker-compose.yml config -q`
Expected: no output, exit 0 (compose schema is valid; the env var is interpolated).

- [ ] **Step 3: Commit**

```bash
git add examples/docker-compose.yml
git commit -m "docs: add sample docker-compose for the gateway (GH #67)"
```

### Task 7: Docs cross-reference

**Files:**
- Modify: `README.md` (add a Docker / releases section pointer)

- [ ] **Step 1: Find a good insertion point**

Run: `grep -n -iE '^#|install|docker|release|build' README.md | head -30`
Expected: locate the install/build section to anchor a new "Container images & releases" subsection near it.

- [ ] **Step 2: Add a short Docker/releases pointer**

Add a subsection (adjust heading level to match the README) referencing the published
images and the sample compose:

```markdown
### Container images & releases

Tagged releases (`vX.Y.Z`) publish cross-platform binaries (with `SHA256SUMS`) to the
GitHub Releases page, and multi-arch Docker images to
`ghcr.io/coffeemuse/tn3270proxy` (tags: `X.Y.Z`, `X.Y`, `X`, and `latest` for stable
releases). See [`examples/docker-compose.yml`](examples/docker-compose.yml) for a
ready-to-run deployment (persistent DB volume, MFA key, config/TLS mounts, and the
first-admin bootstrap step).
```

- [ ] **Step 3: Verify the link target exists**

Run: `test -f examples/docker-compose.yml && echo OK`
Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: reference container images and sample compose (GH #67)"
```

**PR 2 done.** Open a PR. Acceptance:
- `workflow_dispatch` run produces snapshot archives (binaries path proven without publishing).
- Local `docker build` + `docker run ... version` proven in Task 4.
- **Final manual validation (not CI-provable):** push a real low tag (e.g. `v0.1.0`)
  and confirm the GitHub Release has 4 archives + `SHA256SUMS`, `tn3270proxy version`
  reports the tag, and `ghcr.io/coffeemuse/tn3270proxy:0.1.0` + `:latest` exist as a
  2-platform manifest (`docker buildx imagetools inspect`).

---

## Self-review notes

- **Spec coverage:** ci.yml + badge (Task 1–2); GoReleaser binaries/archives/checksums/changelog/prerelease (Task 3); Dockerfile + .dockerignore (Task 4); release.yml binaries + native multi-arch docker + manifest (Task 5); sample compose (Task 6); docs cross-ref (Task 7). All spec sections mapped.
- **No QEMU:** docker job uses `ubuntu-24.04-arm` for arm64 and builds a single platform per runner — no `setup-qemu-action` anywhere. ✓
- **Windows dropped / tar.gz only:** matrix is linux+darwin × amd64+arm64; archives `formats: [tar.gz]`. ✓
- **Image name lowercase:** `ghcr.io/coffeemuse/tn3270proxy` (GHCR requires lowercase owner). ✓
- **Decoupling:** `--skip=docker` on the GoReleaser invocation so it owns only binaries; Docker is fully handled by the buildx matrix + manifest jobs. ✓
