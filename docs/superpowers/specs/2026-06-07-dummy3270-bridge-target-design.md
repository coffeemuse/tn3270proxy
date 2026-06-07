# dummy3270 — Throwaway TN3270 Bridge Target

**Date:** 2026-06-07
**Status:** Approved for spec; pending user review of this document
**Scope:** A minimal standalone TN3270 *server* used as a demo/test backend for the proxy's bridge.

## 1. Purpose

The proxy's whole job is to **bridge** an authenticated user to a backend TN3270 service.
To demo or test that bridge we need *something on the other end*. Today the s3270 smoke
test points its `BACKEND` service at a **second copy of `tn3270proxy` itself**, so the
"backend" actually renders the proxy's own login screen — not a realistic mainframe app,
and a heavyweight fixture (DB, auth, config).

`dummy3270` is a tiny, dependency-light stand-in: a TN3270 server that, on connect,
negotiates Telnet and paints a **mockup welcome screen**. It exists only to give the
bridge a believable, obviously-fake target for demos and protocol testing.

**Non-goals (deliberately out of scope):** authentication, a database, TLS, config files,
per-connection logging, menus that do anything, session state, or any input handling
beyond "repaint on any key." It is a still life, not an application.

## 2. Behavior

1. Listen on a single plaintext TCP port (no TLS).
2. On each connection: perform server-side Telnet/TN3270 negotiation via `go3270`
   (the same negotiation path the proxy's backend leg speaks to).
3. Pick **one of three** canned welcome screens **at random** and paint it.
4. On any returned AID (Enter, a PF key, etc.), pick a **freshly-random** screen and
   repaint. **No state is kept** between paints.
5. On client disconnect (read error), close the connection and end the goroutine.

When reached **through the proxy**, the bridge intercepts **PA3** (the escape AID) and
tears the session down — so `dummy3270` never actually sees PA3. The screens' footer
tells the (bridged) user `Press PA3 to disconnect.` accordingly. Pressed against
`dummy3270` directly (e.g. `c3270` straight at it), PA3 is just another AID and triggers
a repaint; there is no direct-disconnect behavior by design.

## 3. The screens

Three distinct, **obviously-fake** mockups. Each is **pure protected text** — no input
(`Write`) fields — built as a `go3270.Screen`. Proposed flavors (exact wording tunable):

- **A** — a VTAM/CICS-style "good morning" banner (`WELCOME TO CICSDEMO`, region blurb).
- **B** — a fake TSO/ISPF primary-option-menu mockup: numbered options that do nothing.
- **C** — a plain `SYSTEM AVAILABLE` splash with a fake LPAR / SYSID line (static text,
  not a live clock — no state).

**Common to all three:**

- A centered title on row 0.
- A bottom-anchored footer line `Press PA3 to disconnect.` in turquoise.
- A **blinking red `DUMMY3270` marker**, right-aligned on row 0 (col ~71 on a 24×80),
  rendered with `Color: go3270.Red, Highlighting: go3270.Blink`. A following **stop
  field** (default attributes) bounds the marker so the red/blink attribute does not
  bleed into the body. This marker makes it unmistakable that the service is a
  non-working dummy.

Screens target a fixed **24×80** geometry (it's a fixture); content stays within columns
0–79. Alt-size terminals still render — the static layout simply sits in the top-left.

## 4. Architecture (Approach A: thin cmd, logic in `internal/`)

```
cmd/dummy3270/main.go          thin: parse -listen, call dummy.ListenAndServe, exit on error
internal/dummy/server.go       Server: Listen, accept loop, per-conn goroutine (+recover),
                               negotiate, repaint loop
internal/dummy/screens.go      pure builders: screenA/B/C() go3270.Screen, the shared
                               marker+footer helpers, and pick(n int) selecting a screen
internal/dummy/screens_test.go unit tests: content + marker/footer + chooser range
internal/dummy/server_test.go  selection determinism with an injected source
```

All files carry the repo's GPL header. This mirrors the existing convention: `cmd/*` is a
thin wrapper, all real logic lives in a focused `internal/` package, screens are pure
builders (no DB/network), and the package is unit-tested first.

### Components & interfaces

- **`screens.go` — pure builders.** `screenA/B/C() go3270.Screen` and a `builders` slice.
  A `pick(i int) go3270.Screen` (or `pick(rng *rand.Rand)`) returns one builder's screen.
  Randomness is **injectable** — selection takes an index or a `rand` source — so tests
  are deterministic. No I/O. *Depends on:* `go3270` only.
- **`server.go` — net loop.** `ListenAndServe(addr string) error` and an internal
  `Server` with the accept loop. Each connection runs in its own goroutine guarded by
  `recover()` (one bad client cannot crash the listener). Per connection: negotiate via
  `go3270.NegotiateTelnet`, then loop `HandleScreen(pick(...), ... exitKeys, conn)` until
  a read error. Holds a `rand` source. *Depends on:* `net`, `math/rand`, `go3270`,
  `internal/dummy/screens`.
- **`main.go` — CLI.** One flag `-listen` (default `:3300`). Prints **exactly one** line
  to stderr at startup (`dummy3270 listening on :3300`) and nothing per-connection, then
  blocks in `ListenAndServe`. *Depends on:* `flag`, `internal/dummy`.

## 5. Data flow

```
c3270 / proxy bridge ──TCP──▶ dummy3270 listener
        │                          │ accept → goroutine
        │                          ▼
        │                   go3270.NegotiateTelnet(conn)
        │                          │
        ▼                          ▼  loop:
   sees random welcome   ◀──paint── pick(rng) → HandleScreen(screen, …, conn)
   screen w/ blinking      any AID  └────────── repaint (new random screen) ──┐
   DUMMY3270 marker                                                            │
        │                  read err (client gone) → return, close conn ◀───────┘
```

No persistence, no auth, no config — the only inputs are the `-listen` flag and the
inbound TCP stream.

## 6. Error handling

- **Listen failure** (port in use, bad addr): `ListenAndServe` returns the error; `main`
  prints it to stderr and exits non-zero.
- **Per-connection panics / errors:** each connection goroutine `recover()`s and simply
  returns; the listener keeps serving. Negotiation or read errors end that one
  connection's loop cleanly.
- **No retbombs / no logging:** errors on a single connection are swallowed (closed
  quietly) to honor the "no logging" requirement. The fixture's job is to be present and
  paint, not to report.

## 7. Testing (TDD, per repo convention)

- **Screen builders:** for each of A/B/C assert the title text is present, the footer
  reads `Press PA3 to disconnect.`, the `DUMMY3270` marker field is present with
  `Color == go3270.Red` and `Highlighting == go3270.Blink`, and that the screen has **no
  `Write` fields** (purely protected — it must be impossible to type into it).
- **Chooser:** `pick` over indices `0..2` returns each distinct screen; out-of-range / a
  seeded source stays in range. Determinism via an injected index or `rand` source.
- **Net loop:** the thin socket loop is verified via a real emulator / the existing s3270
  smoke approach, **not** unit-tested — the same boundary the repo already draws for
  `go3270` negotiation code.

## 8. Optional follow-up (not in this spec's scope)

Repoint `smoke.sh`'s `BACKEND` service from "a second proxy" to `dummy3270`, so the bridge
scenario lands on a realistic, app-looking screen. This changes the smoke script's grep
expectations for the backend screen and is therefore tracked separately, to be done only
if desired after `dummy3270` lands.
