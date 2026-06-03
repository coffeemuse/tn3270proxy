# CLAUDE.md — TN3270Proxy

Orientation for working in this repo. Read this first in a fresh session.

## What this is

A **TN3270 gateway**. It presents itself as a TN3270 *server* on the (eventually public)
internet, authenticates users against a local SQLite DB, shows a **group-filtered menu**
of internal TN3270 services, and **bridges** the user to the selected backend host. During
a bridged session, **PA3** returns the user to the menu; **PF3** at the menu disconnects.

The connect → login → menu → bridge core loop (the MVP) is **complete and on `main`**.
Remaining work is in `docs/superpowers/ROADMAP.md`.

## Key documents

- **Design/spec:** `docs/superpowers/specs/2026-06-03-tn3270-gateway-mvp-design.md`
- **MVP build plan (done):** `docs/superpowers/plans/2026-06-03-tn3270-gateway-mvp.md`
- **Remaining goals + how to start them:** `docs/superpowers/ROADMAP.md`

## Commands

```bash
go build ./...                 # build
go test ./...                  # all tests
go test ./... -race            # tests with race detector (bridge is concurrent — use this)
go build -o bin/tn3270proxy ./cmd/tn3270proxy

./bin/tn3270proxy seed -db proxy.db -file seed.example.json   # load users/groups/services
./bin/tn3270proxy serve -db proxy.db -listen :2323            # run the proxy
```

Connect with a real 3270 emulator: `c3270 127.0.0.1:2323`.

## Architecture (package map)

```
cmd/tn3270proxy   main: subcommands `serve` (default) and `seed`; wires everything
internal/config   Config{ListenAddr, DBPath}; Load(args)
internal/store    SQLite (modernc, pure-Go). Store + users/groups/services + group-gated
                  ListServicesForGroups. All Create* are idempotent (INSERT OR IGNORE).
internal/auth     Authenticate(ctx, UserStore, user, pass) → Identity{UserID,Username,Groups}.
                  bcrypt; uniform ErrInvalidCredentials (no username-enumeration leak).
internal/screens  Pure go3270 screen builders: LoginScreen(), MenuScreen(svcs, errMsg).
                  Field-name constants: FieldUsername/Password/Error/Selection.
internal/bridge   The bespoke core. telnetProcessor parses one Telnet leg (forward 3270
                  data + IAC IAC / IAC EOR framing; answer negotiation locally; detect PA3
                  escape). Bridge(client, addr, termType, escapeAID) dials backend + relays
                  both ways. Cause = {Error,BackendClosed,ClientClosed,UserEscaped}.
                  Exported escape key: EscapeAIDPA3.
internal/seed     SeedData/SeedUser/SeedService + Apply(): declarative, idempotent seeding.
internal/server   Session state machine (Negotiate→Login→Menu→Bridge loop) behind
                  Presenter/Bridger/Authenticator seams; go3270Presenter + realBridger are
                  the real impls; Server is the TCP accept loop (recovers per-conn panics).
```

Data flow: `main → Server.Serve` (accept) → `Session.Run` → `Presenter` (go3270 screens) /
`Authenticator` (auth+store) / `Bridger` (bridge). The `Presenter`/`Bridger` interfaces exist
so the session is unit-tested with fakes (no live 3270 client needed).

## Conventions

- **TDD.** Every package was built test-first; keep it that way. Run `go test ./... -race`.
- **Small, single-responsibility files**, one package per concern. `store` owns all SQL —
  no SQL leaks elsewhere. `screens` is pure rendering (no DB/network). `auth` never touches
  the network.
- **Interfaces for testability:** `auth.UserStore`, `server.Presenter/Bridger/Authenticator`.
  `*store.Store` satisfies `auth.UserStore`.
- **No credential logging, ever.** Lifecycle logging uses stdlib `log`; usernames are OK to
  log, passwords/Login() contents are not.
- **Commits:** conventional-ish prefixes (`feat:`/`test:`/`chore:`/`docs:`), small and focused.
- **Reserved hooks for future work:** `services.tls` column exists but backend-TLS dialing is
  not implemented; the escape AID is wired through `Session.EscapeAID` / `bridge.EscapeAIDPA3`
  and is meant to become configurable.

## Gotchas (learned the hard way)

- **go3270 cursor:** `HandleScreen`'s initial cursor `(crow, ccol)` must be
  `(field.Row, field.Col + 1)` — a field's `Col` is the **attribute byte**, so input starts
  one column right. `(0,0)` or the field's own `Col` leaves the cursor in the wrong place.
  Unit tests do NOT catch this; only a real emulator does.
- **`go3270.NegotiateTelnet`** ends with a ~10ms read-drain loop that can discard early or
  fragmented client bytes (it runs before app data is expected). `HandleScreen` itself is
  safe (byte-by-byte, stops at IAC EOR). Watch for lost first keystrokes in emulator testing.
- **Bridge is Telnet-aware, not a raw copy:** each leg negotiates Telnet independently, so
  negotiation is answered locally and never forwarded. Do NOT "simplify" it to `io.Copy`.
- **Toolchain:** `modernc.org/sqlite` pulls Go ≥1.25 via the `go` directive; the toolchain
  auto-downloads. No cgo.
- Runtime `*.db` files and `/bin/` are gitignored — don't commit them.

## Verifying a change actually works

Unit tests cover the packages, but the 3270 *protocol surface* (screens, cursor, negotiation,
PA3, bridging) is only truly verified against a live emulator + backend. See the manual
smoke-test checklist in the MVP plan (Task 15) before declaring protocol-facing work done.
