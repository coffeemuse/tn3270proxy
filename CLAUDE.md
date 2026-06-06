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
# release build (stamps version):
go build -ldflags "-X main.version=v1.2.3" -o bin/tn3270proxy ./cmd/tn3270proxy

./bin/tn3270proxy version                                      # print resolved version and exit
./bin/tn3270proxy bootstrap -db proxy.db                      # create first admin (fresh system)
./bin/tn3270proxy seed -db proxy.db -file seed.example.json   # optional: bulk-load users/groups/services
./bin/tn3270proxy serve -db proxy.db -listen :2323            # run the proxy
./bin/tn3270proxy audit list -db proxy.db                      # query the audit trail
./bin/tn3270proxy audit prune -db proxy.db -older-than 90d     # retention cleanup
TN3270PROXY_MFA_KEY=$(openssl rand -base64 32) ./bin/tn3270proxy serve -db proxy.db   # serve with MFA enabled
./bin/tn3270proxy mfa reset-all -db proxy.db                   # break-glass: wipe all MFA enrollments (needs the key)

.claude/skills/s3270-smoke-testing/smoke.sh    # automated 3270 protocol smoke test (s3270)
```

Connect with a real 3270 emulator: `c3270 127.0.0.1:2323`.

## Architecture (package map)

```
cmd/tn3270proxy   main: subcommands `serve` (default), `seed`, `bootstrap`, `version`,
                  `audit list|prune`, and `mfa reset-all` (break-glass); wires everything.
                  `var version = "dev"` is the ldflags injection point (`-X main.version=vX.Y.Z`);
                  resolved via internal/version. serve runs the fail-closed MFA key check
                  (mfaStartup: refuse to start if enrolled users exist but no key, or if the key
                  can't decrypt the MFA_KEY_CHECK sentinel) and injects the *mfa.Cipher.
internal/config   Config{DBPath, Plain, TLS, Limits}; Load(args) merges defaults<file<flags.
                  Optional JSON file (tn3270proxy.json) defines plain+tls listeners and a
                  `limits` section (pre_auth_idle/idle/pre_auth_max as Go duration
                  strings, trusted_cidrs []IP-or-CIDR, bridge_idle "disconnect"|"exempt",
                  max_conns, max_per_ip; defaults 2m/30m/5m, [], disconnect, 512, 16,
                  max_per_ip 0 disables). trusted_cidrs is config-file-only by design
                  (it bypasses DoS controls — deployment surface, not the admin UI).
internal/listen   Build(cfg) → []net.Listener (plaintext + tls.NewListener, immediate TLS).
internal/store    SQLite (modernc, pure-Go). Store + users/groups/services + group-gated
                  ListServicesForGroups. All Create* are idempotent (INSERT OR IGNORE).
                  Names are canonical UPPERCASE: usernames, group names, and service
                  NAMEs fold to upper on create/lookup (the single choke point) and the
                  UNIQUE columns are COLLATE NOCASE. A service has a short uppercase
                  NAME identifier (A-Z/0-9, ≤8, dedup key — NormalizeServiceName) plus a
                  required mixed-case `description` label (≤40 — ValidateDescription);
                  hosts and passwords are NOT normalized.
                  Audit trail: `audit` table (UTC RFC3339, session-correlated) +
                  RecordAudit/ListAudit/PruneAudit.
                  MFA: users carry mfa_required/mfa_secret(encrypted base64, ''=not enrolled)/
                  mfa_enrolled_at/mfa_last_step(replay floor); Set/Clear/StoreMFAEnrollment/
                  UpdateMFAStep/CountEnrolledUsers/ResetAllMFA + Get/SetMFASentinel (the
                  MFA_KEY_CHECK row; not a sysconfig.Catalog entry, hidden from the admin form).
internal/auth     Authenticate(ctx, UserStore, user, pass) → Identity{UserID,Username,Groups}.
                  bcrypt; uniform ErrInvalidCredentials (no username-enumeration leak).
internal/screens  Pure go3270 screen builders: LoginScreen(), MenuScreen(geom, svcs,
                  admin, status, errMsg). The user menu renders an ISPF-style fixed grid
                  (number col 0 / name col 4 / description col 13, hard-cut 40) plus a
                  right-hand status block (MenuStatus: User ID / Date / Time / Terminal /
                  System ID / Release; paint-time clock passed in, not read) at
                  Geometry.StatusBlockCol(). Backend host/port stay hidden (admin-only).
                  All builders take a Geometry (first param; self-normalizing to 24×80)
                  with formula methods for the bottom-anchored rows (HelpRow, ErrorRow,
                  etc.), list page size, form capacity, and menu capacity.
                  Field-name constants: FieldUsername/Password/Error/Selection.
                  Admin screen builders: AdminMenuScreen(), and generic AdminListScreen/
                  AdminFormScreen (paging, line commands, delete confirm).
                  MFA screens: EnrollMFAScreen (issuer/account/chunked key + confirm code; the
                  otpauth URI is deliberately NOT shown — manual entry is the 3270 path) and
                  VerifyMFAScreen; FieldMFACode plus admin FieldMFARequired/Status/Clear.
internal/mfa      Pure TOTP (RFC 6238, pquerna/otp, 80-bit/16-char base32) + AES-256-GCM
                  secret-at-rest. NewCipher/Seal/Open (ErrDecrypt on wrong key), GenerateSecret,
                  Chunk (ABCD EFGH…), Validate(secret, code, lastStep, now) → (ok, step) with
                  ±1-step skew + replay floor. No DB/network. Brute-force throttling
                  lives in internal/server (authThrottle, GH #48); mfa itself has none.
internal/bridge   The bespoke core. telnetProcessor parses one Telnet leg (forward 3270
                  data + IAC IAC / IAC EOR framing; answer negotiation locally; detect PA3
                  escape). Bridge(client, addr, termType, escapeAID, *tls.Config) dials
                  backend (nil = plaintext, non-nil = TLS) + relays both ways.
                  Cause = {Error,BackendClosed,ClientClosed,UserEscaped}.
                  Exported escape key: EscapeAIDPA3.
internal/seed     SeedData/SeedUser/SeedService + Apply(): declarative, idempotent seeding.
internal/version  Resolve(injected) string: returns injected when set by ldflags, otherwise
                  falls back to a 12-char VCS revision from runtime/debug.ReadBuildInfo
                  (+"-dirty" suffix when the working tree is modified). Resolved in
                  cmd/tn3270proxy and threaded as a string into Session.Release (the #53
                  menu status block); no lower package imports it.
internal/server   Session state machine (Negotiate→Login→Menu→Bridge loop) behind
                  Presenter/Bridger/Authenticator seams; go3270Presenter + realBridger are
                  the real impls; Server is the TCP accept loop (recovers per-conn panics);
                  ServeAll runs one Server per listener sharing a handler.
                  Term (negotiated terminal type + alt dimensions + codepage) is returned by
                  Negotiate and threaded through the Presenter and AdminPresenter seams;
                  rendering uses HandleScreenAlt (nil dev → 24×80 fallback).
                  MFA gate (session.go: mfaGate/mfaEnroll/mfaVerify behind EnrollMFA/VerifyMFA
                  Presenter methods) runs AFTER login-success, BEFORE the MOTD/menu — so MFA
                  status never leaks before a correct password. nil Session.MFA disables it;
                  the secret is generated in-memory and persisted (encrypted) only on a correct
                  confirm. PF3/idle returns to login. Session.Now seam makes TOTP deterministic
                  in tests. Login enforcement is secret-first: any stored secret is verified at
                  login regardless of mfa_required (so opt-in MFA is enforced; demoting
                  required=false on an enrolled user keeps verifying until the secret is cleared).
                  adminFlow (admin.go, admin_users.go, admin_groups.go, admin_services.go)
                  behind AdminStore/AdminPresenter seams handles the `A`-entry CRUD flow.
                  Auditor seam (best-effort store-backed auditing; nil disables) +
                  storeAuditor + per-connection auditTrail record session lifecycle events.
                  Hardening (GH #1, #18): idleConn enforces an idle *regime* the session
                  switches at each transition (idleRegime seam: armPreAuth/armPostAuth/
                  armBridge) — pre-auth (idle window + an absolute now+pre_auth_max ceiling
                  that does NOT slide, so a 1-byte/2min trickle can't hold a slot),
                  post-auth (plain idle window), bridge (idle window, or none when
                  bridge_idle=exempt). Post-auth idle at the menu/admin LOGS OUT to the
                  login screen (re-arming pre-auth) rather than disconnecting — audited as
                  logout{idle logout}; PF3-logoff audits as logout{user logoff}. Trusted
                  clients (limits.trusted_cidrs) skip the pre-auth timers and the per-IP cap
                  but still count toward the global cap. connLimiter (shared across listeners
                  by ServeAll(…, Limits)) claims a global slot BEFORE Accept (over-cap conns
                  wait in the kernel backlog) and enforces the per-IP cap after Accept by
                  closing. (Residual: accept-loop rework + user-idle-during-bridge are
                  deferred follow-ups; see #18/#19.)
                  Failed-auth throttling (GH #48): authThrottle (auththrottle.go)
                  applies per-username linear backoff (delay = AUTH_DELAY_BASE_SECS
                  * min(failcount, AUTH_MAX_TRIES), capped at 1h) shared across the
                  password (doLogin) and MFA (mfaVerify/mfaEnroll) failure paths;
                  one instance per handler, keyed on the normalized username
                  (IP-agnostic, so the web client's shared IP is fine), counts
                  reset on success and decay after AUTH_FAIL_WINDOW_MINS. The applied
                  delay is recorded in the auth_fail/mfa_failed audit Detail
                  (delay=Xs count=N). AUTH_DELAY_BASE_SECS=0 disables it.
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
- **Canonical uppercase names.** Usernames, group names, and service NAMEs are folded to
  uppercase in the store layer (the single choke point) so case-insensitive compares done
  Go-side (`slices.Contains(identity.Groups, store.AdminGroup)`) are correct as written.
  Service NAMEs are validated (A-Z/0-9, ≤8); `description` is the user-facing label.
  Passwords and service hosts are never normalized. Pre-prod: schema edited directly, no
  data migration (closed GH #9).
- **No credential logging, ever.** Lifecycle logging uses stdlib `log`; usernames are OK to
  log, passwords/Login() contents are not; `auth.HashPassword` is the single bcrypt path
  (seed + admin UI). Never log the MFA master key, a TOTP secret, or an entered code.
- **MFA master key is infra-level (GH #47):** sourced from `TN3270PROXY_MFA_KEY` (base64 of
  32 bytes) or config `mfa.key`/`mfa.key_file` (env wins), NEVER the runtime DB params. `serve`
  refuses to start if enrolled users exist but no key is set, or if the key can't decrypt the
  `MFA_KEY_CHECK` sentinel (wrong/rotated key). Recover a lost key with `tn3270proxy mfa
  reset-all` (wipes all enrollments; everyone re-enrolls). TOTP secrets are stored AES-256-GCM
  encrypted. ±1-step/replay is NOT a brute-force defense; per-attempt throttling is in
  internal/server (authThrottle, GH #48) — see the package map.
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
  Builders now own this (GH #33): each screen builder returns a `screens.Cursor` computed
  via the single `cursorAt(field)` helper (`internal/screens/cursor.go`), and presenters
  consume it instead of hardcoding coordinates — so moving a field moves its cursor
  automatically (an empty admin list homes to `{0,0}`). The smoke script asserts cursor
  row/col per screen as the regression guard.
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
  only the plain addr (when the plain listener is enabled); if a config file sets
  `listeners.plain.enabled=false`, passing `-listen` is a fatal conflict error — it never
  silently enables plaintext on a gateway that deliberately disabled it. Pass `-config`
  with `"tls":{"enabled":false}` for throwaway instances (smoke.sh does this).
- Runtime `*.db` files and `/bin/` are gitignored — don't commit them.
- **MOTD/NEWS screen is deliberately chrome-less** (`internal/screens/news.go`):
  no title row, no PF-key help row — just red protected text and a `***` page
  gate on the last row, matching classic TSO/READY logon messages. This is an
  intentional exception to the "title on row 0, PF help on the last row" rule.
  Lines truncate at column 79; the gate sits on `NewsLinesPerPage()+1`.

## Verifying a change actually works

Unit tests cover the packages, but the 3270 *protocol surface* (screens, cursor, negotiation,
PA3, bridging) is only truly verified against a real emulator + backend. The
**s3270-smoke-testing skill** (`.claude/skills/s3270-smoke-testing/`) automates the Task 15
checklist with s3270 (screen content, cursor position, non-display attributes, PA3/PF3,
bridging) — run it before declaring protocol-facing work done. A human pass in a live
emulator (c3270) remains the final word on visual polish.
