# Comment & Readability Pass Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (the spec explicitly rejects parallel subagents for this work — comment accuracy needs accumulated in-session context). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Comments-only quality pass over the whole codebase (3 layers: package docs, godoc, inline why/domain comments) so a junior dev who knows Go but not 3270 can navigate it; every existing comment verified accurate.

**Architecture:** A charter doc (`docs/comment-style.md`) defines the standard; then 5 sequential comments-only PRs apply it package-group by package-group, front door first. Each file gets a read → verify → edit pass; each PR gates on build/vet/gofmt/race-tests plus a comments-only diff check, and its description enumerates accuracy corrections.

**Tech Stack:** Go 1.25, git/gh. No new dependencies, no behavioral code changes anywhere in the series.

**Spec:** `docs/superpowers/specs/2026-06-10-comment-readability-pass-design.md`

---

## The shared per-file recipe (referenced by every package step)

For **each file** in a package batch:

1. **Read the whole file** (not a skim — every function).
2. **Verify every existing substantive comment** against the code below it:
   - A comment claiming a number/limit/default (`8 KiB cap`, `defaults 2m/30m/5m`) → grep for the constant and confirm.
   - A comment claiming behavior (`PF3 returns to the menu`) → find the code path or the test that proves it (`grep -rn <relevant term> --include='*_test.go'`).
   - A comment that smells stale → `git log -L<start>,<end>:<file>` to see if code changed after the comment was written.
   - Stale → fix the comment now and **record it in `/tmp/comment-pass-findings.md`** (file + line + was/now) for the PR description.
3. **Apply the three layers** (per `docs/comment-style.md`):
   - Package comment present? (Only on one file per package; `doc.go` for `server`, `store`, `ui3270`, `screens`, `quickstart`; otherwise atop the package's primary file.)
   - Every exported symbol gets godoc: complete sentence starting with the symbol name; contract not implementation (inputs, zero value, error semantics, concurrency).
   - Inline: add **why/domain/invariant** comments at genuinely tricky code; one-line 3270/telnet/ISPF explanations with pointer (RFC 1576/2355, `docs/ispf-style-guide.md`) where a Go dev would be lost.
4. **Delete noise**: narration (`// loop over services`), diff archaeology (`// new in #79`), reviewer-talk. Deletions are progress, not loss.
5. **Refactor temptation** (long function, bad name, dead code) → do NOT touch it; add to `/tmp/comment-pass-findings.md` under "Issues to file".
6. **CLAUDE.md disagreement** (code contradicts the package map) → record under "CLAUDE.md discrepancies" in the findings file; do not edit CLAUDE.md until Task 6.

**Godoc coverage check** (run per package; expected output: nothing, or only lines you can justify, e.g. grouped consts under one doc comment):

```bash
awk 'FNR==1{prev=""} (/^func (\([^)]*\) )?[A-Z]/ || /^(type|const|var) [A-Z]/) && prev !~ /^\/\// {print FILENAME":"FNR": "$0} {prev=$0}' $(find <PKG> -name '*.go' ! -name '*_test.go')
```

**Quality gates** (run after each package batch; ALL must pass before commit):

```bash
gofmt -l .                  # expected: no output
go build ./...              # expected: exit 0, silent
go vet ./...                # expected: exit 0, silent
go test ./... -race         # expected: ok for every package, no FAIL
```

**Comments-only diff check** (run before opening each PR):

```bash
git diff main...HEAD -- '*.go' | grep -E '^[+-][^+-]' | grep -vE '^[+-]\s*(//|$)' | grep -vE '^[+-]package [a-z0-9_]+$'
```

Expected: **empty**, OR only paired lines where the code left of `//` is character-identical on the `-` and `+` sides (a trailing-comment edit). Anything else = a behavioral change leaked in; revert it.

**PR body template** (every PR in the series):

```markdown
## Comment & readability pass — <group> (PR <n>/5)

Comments-only diff per docs/comment-style.md and the spec
(docs/superpowers/specs/2026-06-10-comment-readability-pass-design.md).
No behavioral changes; build/vet/gofmt/`go test ./... -race` green.

### Accuracy corrections (reviewers: check these)
- `<file>:<line>` — said <old claim>; code actually <verified behavior>. Fixed.

### Added
- package docs: <list>
- godoc: all exported symbols in <packages>
- domain/why comments: <notable spots>

### Deleted
- <n> narration/archaeology comments

### Issues filed / noted
- <links or "none">

🤖 Generated with [Claude Code](https://claude.com/claude-code)
```

---

### Task 1: Comment charter

**Files:**
- Create: `docs/comment-style.md`

- [ ] **Step 1: Write the charter** with exactly this content:

```markdown
# Comment Style — TN3270Proxy

How we comment this codebase. Applies to all Go code, tests included. The audience
for every comment is a competent Go developer who has never seen a 3270 terminal.

## The three layers

1. **Package comments.** Every package has one, attached to `package <name>`
   (in `doc.go` for packages with more than a few files). It answers: what is this
   package, where does it sit in the gateway's data flow (connect → login → menu →
   bridge), and what does it deliberately *not* do. 3–10 lines.
2. **Godoc on every exported symbol.** Standard Go form: a complete sentence
   starting with the symbol's name. Document the contract — inputs, zero value,
   error semantics, concurrency — not the implementation.
3. **Inline comments** only where the code cannot speak for itself:
   - **Why, not what:** the reason this approach was chosen, the bug it avoids,
     the invariant it preserves.
   - **Domain:** 3270/telnet/ISPF facts a Go developer cannot be expected to know.
     One line, plus a pointer where depth exists (RFC 1576/2355,
     docs/ispf-style-guide.md).
   - **Cross-cutting invariants:** secret-first MFA verification, canonical
     UPPERCASE names, the idle-regime state machine, the go3270 cursor Col+1
     rule, and similar — these are not googleable and live here.

## What we never write

- **Narration:** `// loop over the services`, `// return the error`. Delete on sight.
- **Diff archaeology:** `// changed in PR #79`, `// new approach`. Git owns history.
- **Reviewer-talk:** comments explaining why a change is correct. That belongs in
  the PR description.
- **Go tutoring:** assume the reader knows goroutines, defer, error wrapping.

## Accuracy

A wrong comment is worse than no comment — readers trust comments over code.
When you change behavior, update every comment that describes it (grep for the
old terms). If you find a stale comment, fix it in the same PR and call it out
in the PR description.

## Tests

Test comments state scenario intent — the invariant the test protects ("a
settings-locked user must never be force-enrolled, even when mfa_required is
set") — not mechanics.

## Maintenance

New exported symbol → godoc in the same commit. New package → package comment in
the same commit. Review treats a missing or stale comment as a defect like any
other.
```

- [ ] **Step 2: Commit**

```bash
git add docs/comment-style.md docs/superpowers/plans/2026-06-10-comment-readability-pass.md
git commit -m "docs: comment style charter + comment-pass implementation plan"
```

---

### Task 2: PR 1 — front door (`cmd/*`, `config`, `listen`, `logging`, `version`)

Work on the current branch (`claude/quirky-ride-094e66`), which already carries the spec.
Create `/tmp/comment-pass-findings.md` (headings: Accuracy corrections / Issues to file /
CLAUDE.md discrepancies) before starting.

**Files (modify, comments only):**
- `cmd/tn3270proxy/main.go` (154), `audit.go` (146), `bootstrap.go` (115), `mfa.go` (142), `quickstart.go` (40), `version.go` (45)
- `cmd/dummy3270/main.go` (40)
- `internal/config/config.go` (380)
- `internal/listen/listen.go` (75)
- `internal/logging/logging.go` (117)
- `internal/version/version.go` (72)

- [ ] **Step 1: `cmd/tn3270proxy`** — apply the shared per-file recipe to all 6 files. This is the package a new dev reads first: the package comment (atop `main.go`) must map subcommand → purpose → key flags in a few lines. Run the godoc coverage check with `<PKG>=cmd/tn3270proxy`.
- [ ] **Step 2: `cmd/dummy3270` + `internal/listen` + `internal/logging` + `internal/version`** — recipe per file; package comment atop each package's single/primary file. Coverage check each.
- [ ] **Step 3: `internal/config`** — recipe; at 4% density this needs the most new material. The package comment must state the merge order (defaults < file < flags) and that `trusted_cidrs` is config-file-only by design; each Config field gets a godoc line with its default.
- [ ] **Step 4: Quality gates** — run all four commands from the recipe header; all green.
- [ ] **Step 5: Comments-only diff check** — run; resolve any non-comment lines.
- [ ] **Step 6: Commit + PR**

```bash
git add -A && git commit -m "docs(comments): front-door packages — cmd, config, listen, logging, version (pass 1/5)"
git push -u origin claude/quirky-ride-094e66
gh pr create --title "Comment & readability pass 1/5: charter + front-door packages" --body-file <(fill PR template from /tmp/comment-pass-findings.md)
```

---

### Task 3: PR 2 — `internal/server`

```bash
git checkout main && git pull && git checkout -b comment-pass-2-server
```

Reset `/tmp/comment-pass-findings.md`. 22 files / 4,342 lines — four batches, commit after each.

- [ ] **Step 1: Batch A (core):** `server.go` (258), `session.go` (1053). Create `internal/server/doc.go` with the package comment: the session state machine (Negotiate→Login→[MFA]→MOTD→Menu→Bridge), the Presenter/Bridger/Authenticator seams and why they exist (unit-testing without a live 3270 client), and a pointer to the spec. In `session.go`, the secret-first MFA ordering and the settings-locked skip MUST each carry a why-comment. Recipe + coverage check, then `git commit -m "docs(comments): server core — server.go, session.go, doc.go"`.
- [ ] **Step 2: Batch B (connection infrastructure):** `idleconn.go` (121), `limiter.go` (132), `trust.go` (100), `registry.go` (161), `resolver.go` (63), `term.go` (62), `handle_screen.go` (75). The idle-regime contract (pre-auth ceiling does NOT slide; post-auth idle logs out rather than disconnecting) and the limiter's claim-before-Accept design each need a why-comment. Recipe + coverage check, commit.
- [ ] **Step 3: Batch C (seams + auth):** `presenter.go` (230), `presenter_admin.go` (76), `presenter_mfa.go` (66), `auditor.go` (103), `auththrottle.go` (232). authThrottle's username-keyed (IP-agnostic) design rationale needs a why-comment. Recipe + coverage check, commit.
- [ ] **Step 4: Batch D (admin flows):** `admin.go` (183), `admin_users.go` (416), `admin_groups.go` (182), `admin_services.go` (235), `admin_sessions.go` (200), `admin_audit.go` (184), `admin_networks.go` (120), `admin_system.go` (90). ZZADMIN guardrails (no self-delete, never-empty ZZADMIN, no self-demotion) get why-comments at enforcement sites. Recipe + coverage check, commit.
- [ ] **Step 5: Gates + diff check + PR** — all four gates, comments-only check, then `gh pr create --title "Comment & readability pass 2/5: internal/server"` with the PR template.

---

### Task 4: PR 3 — protocol surface (`bridge`, `screens`, `ui3270`, `dummy`)

```bash
git checkout main && git pull && git checkout -b comment-pass-3-protocol
```

Reset the findings file. This is the most domain-heavy PR — budget the most new comment text here.

- [ ] **Step 1: `internal/bridge`** — `bridge.go` (131), `telnet.go` (230). Package comment atop `bridge.go`: why this is NOT io.Copy (each leg negotiates Telnet independently; negotiation answered locally, never forwarded) — the #1 thing a junior must not "simplify". `telnet.go`: IAC IAC / IAC EOR framing gets a short prose intro (what EOR means in TN3270, pointer to RFC 1576). Recipe + coverage check, commit.
- [ ] **Step 2: `internal/screens`** — 9 files (844). `doc.go`: pure rendering, no DB/network; the three-band ISPF convention + the two documented exceptions (login, MOTD/NEWS) with pointer to `docs/ispf-style-guide.md`; 0-based rows; the cursor Col+1 gotcha (field Col = attribute byte). Recipe per file + coverage check, commit.
- [ ] **Step 3: `internal/ui3270`** — 11 files (1300). `doc.go`: the Renderer seam and why admin + self-service share one paging/line-command engine. Recipe + coverage check, commit.
- [ ] **Step 4: `internal/dummy`** — `screens.go` (102), `server.go` (106). Package comment: deliberately stateless/no-auth/no-logging demo target. Recipe + coverage check, commit.
- [ ] **Step 5: Gates + diff check + PR** — `gh pr create --title "Comment & readability pass 3/5: protocol surface — bridge, screens, ui3270, dummy"`.

---

### Task 5: PR 4 — data + security (`store`, `auth`, `mfa`, `sysconfig`, `seed`, `quickstart`)

```bash
git checkout main && git pull && git checkout -b comment-pass-4-data
```

Reset the findings file.

- [ ] **Step 1: `internal/store`** — 8 files (1457). `doc.go`: owns ALL SQL (no SQL leaks elsewhere); canonical-UPPERCASE choke point; migration ledger rules (append-only, forward-only, `reconcileDefaults` for code-defined seeds — and WHY defaults live outside the ledger: they must reach already-migrated DBs). `migrate.go` gets a prose intro on the ledger contract; `trusted_networks.go` is new and likely thin — full treatment. Recipe + coverage check, commit.
- [ ] **Step 2: `internal/auth` + `internal/mfa`** — `auth.go` (99); `cipher.go` (86), `totp.go` (102). Why-comments: uniform `ErrInvalidCredentials` + dummy-hash timing equalization (anti-enumeration); ±1-step skew + replay floor is NOT brute-force protection (that's authThrottle in server). Recipe + coverage check, commit.
- [ ] **Step 3: `internal/sysconfig` + `internal/seed`** — `catalog.go` (233), `seed.go` (155). Why MFA_KEY_CHECK is deliberately NOT a Catalog entry. Recipe + coverage check, commit.
- [ ] **Step 4: `internal/quickstart`** — 9 files (420). `doc.go`: provisioning detection contract (config written LAST as the marker) and "not the production path". Recipe + coverage check, commit.
- [ ] **Step 5: Gates + diff check + PR** — `gh pr create --title "Comment & readability pass 4/5: data + security — store, auth, mfa, sysconfig, seed, quickstart"`.

---

### Task 6: PR 5 — test files + CLAUDE.md reconciliation

```bash
git checkout main && git pull && git checkout -b comment-pass-5-tests
```

Reset the findings file. 81 test files / ~15.8k lines. Comment standard for tests: a one-line scenario-intent comment on each non-obvious Test function; table-driven cases get intent in the case name or a terse field comment; delete mechanical narration. Most short tests need NOTHING — a good test name is the comment. If the diff exceeds ~2.5k changed lines, split at the natural boundary (5a: `server` + `store` tests; 5b: the rest) — two PRs, same template.

- [ ] **Step 1: `internal/server` tests** (18 files, ~6.1k lines — the big ones: `admin_test.go` 1494, `session_test.go` 1438). Recipe steps 1–2 and 4–6 apply (verify + delete noise); layer 3 is scenario-intent only. Commit.
- [ ] **Step 2: `internal/store` + `internal/screens` + `internal/ui3270` tests** (~5.5k lines). Commit.
- [ ] **Step 3: remaining tests** (`cmd/*`, `auth`, `bridge`, `config`, `dummy`, `listen`, `logging`, `mfa`, `quickstart`, `seed`, `sysconfig`, `version`; ~4.2k lines). Commit.
- [ ] **Step 4: CLAUDE.md reconciliation** — dedicated commit applying every entry accumulated under "CLAUDE.md discrepancies" across all five findings files (known already: the `internal/server` package map omits `registry.go`, `trust.go`, `resolver.go`, `term.go`, `handle_screen.go`, `admin_networks.go`, `admin_audit.go`, `admin_sessions.go`, `admin_system.go`; `store` omits `trusted_networks.go`). `git commit -m "docs: reconcile CLAUDE.md package map with reality (comment-pass findings)"`.
- [ ] **Step 5: File the accumulated issues** — for each "Issues to file" entry: `gh issue create --title "<imperative title>" --body "<finding + file:line + why it's out of comment-pass scope>"`.
- [ ] **Step 6: Gates + diff check + PR** — `gh pr create --title "Comment & readability pass 5/5: tests + CLAUDE.md reconciliation"`.

---

## Self-review notes

- Spec coverage: charter (Task 1), 3 layers + accuracy + tests (recipe + Tasks 2–6), per-package PR series in spec order (Tasks 2–6), comments-only guarantee (diff check in every task), stale-comment enumeration (findings file → PR template), CLAUDE.md-disagreement handling (recipe item 6 → Task 6 Step 4). PR-split escape hatch for tests preserved.
- The gates are identical in every task by design — repetition is the point (no "see Task 2").
- Risk: trailing-comment edits trip the diff check — handled by the explicit paired-line exception.
