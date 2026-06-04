# CLAUDE.md — TN3270Proxy

Orientation for working in this repo. Read this first in a fresh session.

## What this is

A **TN3270 gateway**. It presents itself as a TN3270 *server* on the (eventually public)
internet, authenticates users against a local SQLite DB, shows a **group-filtered menu**
of internal TN3270 services, and **bridges** the user to the selected backend host. During
a bridged session, **PA3** returns the user to the menu (PA3 does nothing
on the proxy's own screens). **PF3** uniformly steps back one level: admin
sub-screen → admin menu → service menu → login screen → disconnect; PF3 at the
service menu is a logoff, and re-login re-evaluates groups.
Members of the reserved `ZZADMIN` group get an extra `A` menu entry opening a full-CRUD admin screen set (users / groups / services).

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
./bin/tn3270proxy audit list -db proxy.db                      # query the audit trail
./bin/tn3270proxy audit prune -db proxy.db -older-than 90d     # retention cleanup

.claude/skills/s3270-smoke-testing/smoke.sh    # automated 3270 protocol smoke test (s3270)
```

Connect with a real 3270 emulator: `c3270 127.0.0.1:2323`.

## Architecture (package map)

```
cmd/tn3270proxy   main: subcommands `serve` (default), `seed`, and `audit list|prune`; wires everything
internal/config   Config{DBPath, Plain, TLS, Limits}; Load(args) merges defaults<file<flags.
                  Optional JSON file (tn3270proxy.json) defines plain+tls listeners and a
                  `limits` section (pre_auth_idle/idle as Go duration strings, max_conns,
                  max_per_ip; defaults 2m/30m/512/16, max_per_ip 0 disables).
internal/listen   Build(cfg) → []net.Listener (plaintext + tls.NewListener, immediate TLS).
internal/store    SQLite (modernc, pure-Go). Store + users/groups/services + group-gated
                  ListServicesForGroups. All Create* are idempotent (INSERT OR IGNORE).
                  Audit trail: `audit` table (UTC RFC3339, session-correlated) +
                  RecordAudit/ListAudit/PruneAudit.
internal/auth     Authenticate(ctx, UserStore, user, pass) → Identity{UserID,Username,Groups}.
                  bcrypt; uniform ErrInvalidCredentials (no username-enumeration leak).
internal/screens  Pure go3270 screen builders: LoginScreen(), MenuScreen(svcs, errMsg).
                  All builders take a Geometry (first param; self-normalizing to 24×80)
                  with formula methods for the bottom-anchored rows (HelpRow, ErrorRow,
                  etc.), list page size, form capacity, and menu capacity.
                  Field-name constants: FieldUsername/Password/Error/Selection.
                  Admin screen builders: AdminMenuScreen(), and generic AdminListScreen/
                  AdminFormScreen (paging, line commands, delete confirm).
internal/bridge   The bespoke core. telnetProcessor parses one Telnet leg (forward 3270
                  data + IAC IAC / IAC EOR framing; answer negotiation locally; detect PA3
                  escape). Bridge(client, addr, termType, escapeAID, *tls.Config) dials
                  backend (nil = plaintext, non-nil = TLS) + relays both ways.
                  Cause = {Error,BackendClosed,ClientClosed,UserEscaped}.
                  Exported escape key: EscapeAIDPA3.
internal/seed     SeedData/SeedUser/SeedService + Apply(): declarative, idempotent seeding.
internal/server   Session state machine (Negotiate→Login→Menu→Bridge loop) behind
                  Presenter/Bridger/Authenticator seams; go3270Presenter + realBridger are
                  the real impls; Server is the TCP accept loop (recovers per-conn panics);
                  ServeAll runs one Server per listener sharing a handler.
                  Term (negotiated terminal type + alt dimensions + codepage) is returned by
                  Negotiate and threaded through the Presenter and AdminPresenter seams;
                  rendering uses HandleScreenAlt (nil dev → 24×80 fallback).
                  adminFlow (admin.go, admin_users.go, admin_groups.go, admin_services.go)
                  behind AdminStore/AdminPresenter seams handles the `A`-entry CRUD flow.
                  Auditor seam (best-effort store-backed auditing; nil disables) +
                  storeAuditor + per-connection auditTrail record session lifecycle events.
                  Hardening (GH issue #1): idleConn wraps every conn and arms an idle
                  deadline around each Read/Write (pre-auth window → wider post-auth via
                  the idleSetter seam; deadline-fired disconnects audit as "idle timeout");
                  connLimiter (shared across listeners by ServeAll(…, Limits)) claims a
                  global slot BEFORE Accept (over-cap conns wait in the kernel backlog)
                  and enforces the per-IP cap after Accept by closing.
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
- **Reserved groups:** `ZZ*` (case-insensitive, `store.ReservedGroupPrefix`) group names are
  app-dictated; `store.AdminGroup` (`ZZADMIN`) is auto-created by `migrate()`. The admin UI
  can't create/delete `ZZ*` groups, only manage membership. Guardrails: no self-delete, never
  empty ZZADMIN, no self-demotion from ZZADMIN. Admin changes apply at the next menu render/login — live sessions are not
  re-evaluated.
- **No credential logging, ever.** Lifecycle logging uses stdlib `log`; usernames are OK to
  log, passwords/Login() contents are not; `auth.HashPassword` is the single bcrypt path
  (seed + admin UI).
- **Commits:** conventional-ish prefixes (`feat:`/`test:`/`chore:`/`docs:`), small and focused.
- **Backend TLS:** implemented. A service dials over TLS when `services.tls` is set; the
  per-service `services.tls_verify` column (default on) controls certificate verification
  (system roots + hostname, browser-like). `verify:false` in seed JSON encrypts without
  authenticating (for self-signed internal hosts). ServerName is always the configured
  host — connect-by-IP with verify on needs an IP SAN. The escape AID
  (`Session.EscapeAID` / `bridge.EscapeAIDPA3`) remains the reserved hook meant to become
  configurable.

## Gotchas (learned the hard way)

- **go3270 cursor:** `HandleScreen`'s initial cursor `(crow, ccol)` must be
  `(field.Row, field.Col + 1)` — a field's `Col` is the **attribute byte**, so input starts
  one column right. `(0,0)` or the field's own `Col` leaves the cursor in the wrong place.
  Unit tests do NOT catch this; only a real emulator does (s3270's status line reports
  cursor row/col — the smoke script asserts it; see the s3270-smoke-testing skill).
- **Screen layout convention** (all screens are 0-based, 24 rows = 0..23): title on **row 0**,
  the **error line just above** the action/help line, and the **PF-key help on the last row
  (`geom.HelpRow()`; 23 on a MOD 2)**. Cursor lands on the primary input field per the rule
  above. New screens must take a `screens.Geometry` instead of hard-coding row numbers —
  MOD 3/4/5 clients get taller layouts, content stays within columns 0–79. Unit tests assert
  field *names/content*, not row numbers — verify positioning in a real emulator.
- **`go3270.NegotiateTelnet`** ends with a ~10ms read-drain loop that can discard early or
  fragmented client bytes (it runs before app data is expected). `HandleScreen` itself is
  safe (byte-by-byte, stops at IAC EOR). Watch for lost first keystrokes in emulator testing.
- **Bridge is Telnet-aware, not a raw copy:** each leg negotiates Telnet independently, so
  negotiation is answered locally and never forwarded. Do NOT "simplify" it to `io.Copy`.
- **Toolchain:** `modernc.org/sqlite` pulls Go ≥1.25 via the `go` directive; the toolchain
  auto-downloads. No cgo.
- **`tn3270proxy.json` in the repo root is auto-loaded by `serve`** and enables a TLS
  listener on :2324 — a second instance collides with a running one. `-listen` overrides
  only the plain addr; pass `-config` with `"tls":{"enabled":false}` for throwaway
  instances (smoke.sh does this).
- Runtime `*.db` files and `/bin/` are gitignored — don't commit them.

## Verifying a change actually works

Unit tests cover the packages, but the 3270 *protocol surface* (screens, cursor, negotiation,
PA3, bridging) is only truly verified against a real emulator + backend. The
**s3270-smoke-testing skill** (`.claude/skills/s3270-smoke-testing/`) automates the Task 15
checklist with s3270 (screen content, cursor position, non-display attributes, PA3/PF3,
bridging) — run it before declaring protocol-facing work done. A human pass in a live
emulator (c3270) remains the final word on visual polish.
