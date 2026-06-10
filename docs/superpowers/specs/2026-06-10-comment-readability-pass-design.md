# Comment & Readability Pass — Design

**Date:** 2026-06-10
**Status:** Approved
**Driver:** A junior-level team member may join soon and must be able to understand the
codebase from the code itself. Comments must be accurate (active development has likely
left some stale), clarifying, and free of noise.

## Goal

A quality pass — not a density pass — over all comments in the non-test and test Go code:

1. **Package layer:** every package gets a package comment (a `doc.go` for the larger
   packages) covering what it is, where it sits in the data flow, and what it deliberately
   does *not* do.
2. **API layer:** every exported symbol gets godoc in standard Go form (first word = symbol
   name), so `go doc` and editor hovers work.
3. **Logic layer:** inline comments at genuinely tricky spots explain *why* and *domain*,
   never *what the next line does*. Narration comments are deleted, not preserved.
4. **Accuracy:** every substantive existing comment is verified against the code it
   describes; stale comments are corrected and called out in the PR description.
5. **Tests:** test-file comments state *scenario intent* (the invariant being protected),
   not mechanics.

## Audience calibration

**Explain the domain, not Go.** Assume Go competence; never assume 3270/telnet/ISPF
knowledge. Obscure protocol facts get a one-line explanation or a pointer (RFC 1576/2355,
`docs/ispf-style-guide.md`). Project invariants (idle regimes, secret-first MFA, canonical
uppercase, the go3270 cursor `Col+1` rule, etc.) are prime comment material — they are not
googleable.

## Non-goals

- No identifier renames, no code motion, no refactoring. Diffs are comments-only
  (plus the charter doc and `doc.go` files). Anything tempting gets filed as a GitHub
  issue instead.
- No duplication of CLAUDE.md into the code. CLAUDE.md remains the AI-orientation doc;
  where a code comment and CLAUDE.md disagree, that is a finding to resolve, not copy.
- No raising comment percentage for its own sake.

## Approach (selected: deep read-and-edit, per package)

For each package, in priority order:

1. Read every file end-to-end.
2. Verify each existing substantive comment against the code it describes — grep for
   claimed constants/behaviors, cross-check tests, use `git log`/blame when a comment
   smells stale. Every behavioral claim in a *new* comment must be verified in-session,
   not recalled from CLAUDE.md or memory.
3. Apply all three comment layers in one pass.
4. Gate: `go build ./... && go vet ./... && gofmt -l .` clean, full `go test ./... -race`
   green, and a diff check confirming only comment/doc lines changed.
5. PR whose description explicitly lists **accuracy corrections** (the diffs needing real
   reviewer attention; new godoc is low-risk by comparison).

Rejected alternatives: audit-first/edit-second (doubles the passes; the comments-only diff
*is* the audit report) and parallel subagents (comment accuracy needs accumulated project
context; paraphrasing half-understood code produces exactly the plausible-but-wrong
comments this pass exists to remove).

## Comment charter

Before any code is touched, write `docs/comment-style.md` (~1 page) codifying the rules
above so the standard survives this pass: future contributors (human and AI) maintain it.
The charter is the first deliverable and lands in PR 1.

## Delivery: PR series

| PR | Packages | Rationale |
|----|----------|-----------|
| 1 | charter + `cmd/tn3270proxy`, `cmd/dummy3270`, `internal/config`, `internal/listen`, `internal/logging`, `internal/version` | The front door — first code a new person reads; currently thinnest (2–6% density) |
| 2 | `internal/server` | The core loop; biggest (4.3k lines) and most invariant-dense |
| 3 | `internal/bridge`, `internal/screens`, `internal/ui3270`, `internal/dummy` | The 3270 protocol surface — most domain explanation needed |
| 4 | `internal/store`, `internal/auth`, `internal/mfa`, `internal/sysconfig`, `internal/seed`, `internal/quickstart` | Data + security layer |
| 5 | test files, same grouping | Scenario-intent comments |

Each PR is independently reviewable and the series can stop at any point with value
delivered.

## Success criteria

- Charter exists and is followed by every PR in the series.
- Every package has a package comment; every exported symbol has godoc.
- Zero behavioral code changes across the series (comments-only diffs).
- All stale comments found are corrected and enumerated in PR descriptions.
- Build, vet, gofmt, and `-race` tests green at every PR.
