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
