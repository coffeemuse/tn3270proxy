# Backend-side TLS dialing — Design

**Status:** Approved (brainstorm complete)
**Date:** 2026-06-03
**Roadmap item:** #2 (`docs/superpowers/ROADMAP.md`)
**Spec for:** connecting the bridge to backend services over TLS when a service is
marked TLS, with a deliberate per-service certificate-verification policy.

## Goal

When a menu service is marked TLS, dial the backend over TLS instead of plaintext.
The `services.tls` column is already plumbed through `store.Service.TLS`, the seed
format, and the menu display — only the bridge's dial ignores it. This milestone makes
the bridge honor it, and adds a **per-service certificate-verification switch** so the
operator can connect to internal hosts whose certs are self-signed or chain to a private
CA.

## Verification policy (the core decision)

- Verification is **per service**, an on/off switch, **defaulting to on**.
- When **on**, the backend cert is validated **like a browser**: against the system root
  store, with hostname matching against the cert's CN/SAN. No custom CA bundle.
- When **off**, the leg is **encrypted but not authenticated** (`InsecureSkipVerify`) —
  for internal hosts with self-signed certs.
- **ServerName is always the configured service host.** There is no per-service
  ServerName override. Consequence: a service defined by **IP address** with verify
  **on** requires the cert to carry that IP in a SAN; otherwise turn verify **off** for
  that one service. (A ServerName override and a configurable CA bundle were considered
  and deliberately deferred — YAGNI for v1.)

## Architecture

The change touches four packages; the dependency direction keeps `crypto/tls` out of the
session state machine.

```
store    + tls_verify column, guarded migration, Service.TLSVerify, CreateService(verify)
seed     + SeedService.Verify *bool (nil → true), Apply resolves it
bridge   Bridge(..., tlsCfg *tls.Config): nil → plaintext (today), non-nil → tls dial
server   Bridger interface carries BackendTLS{Enabled,Verify} intent;
         realBridger translates intent → *tls.Config; session passes intent
```

### 1. Data model & schema (`internal/store`)

- New column: `services.tls_verify INTEGER NOT NULL DEFAULT 1`.
- `migrate()` must stay idempotent. `CREATE TABLE IF NOT EXISTS` will **not** add a column
  to an already-existing `services` table, so migration becomes:
  1. Run the existing `schema` (creates tables on a fresh DB; for a fresh DB the
     `services` `CREATE TABLE` should also include `tls_verify` so new DBs get it
     directly).
  2. Query `PRAGMA table_info(services)`; if `tls_verify` is absent, run
     `ALTER TABLE services ADD COLUMN tls_verify INTEGER NOT NULL DEFAULT 1`.
  - Existing rows inherit `1` (verify on) — the secure default. Running `migrate()`
    repeatedly is a no-op.
- `store.Service` gains `TLSVerify bool`, populated everywhere a service is SELECTed
  (notably `ListServicesForGroups`).
- `CreateService(ctx, name, host, port, tls, verify bool)` — new trailing `verify` param,
  written to the new column.

### 2. The dial (`internal/bridge`)

- `Bridge` gains a trailing parameter:
  `Bridge(client net.Conn, addr, termType string, escapeAID byte, tlsCfg *tls.Config) (Cause, error)`.
- `tlsCfg == nil` → unchanged plaintext path (`net.DialTimeout("tcp", addr, dialTimeout)`).
- `tlsCfg != nil` →
  `tls.DialWithDialer(&net.Dialer{Timeout: dialTimeout}, "tcp", addr, tlsCfg)`. The
  handshake completes within `dialTimeout`.
- Everything downstream is untouched: `*tls.Conn` satisfies `net.Conn`, so the
  Telnet-aware relay, the `pastDeadline` teardown, and deadline resets all work as-is. Do
  **not** simplify the relay.

### 3. Wiring & interface (`internal/server`)

To keep `crypto/tls` out of the session machine and its fakes, the `Bridger` interface
carries TLS **intent**, not a constructed config:

```go
// BackendTLS expresses a service's backend-TLS intent.
type BackendTLS struct {
    Enabled bool // dial over TLS
    Verify  bool // validate the backend cert (system roots + hostname)
}

type Bridger interface {
    Bridge(client net.Conn, addr, termType string, escapeAID byte, btls BackendTLS) (bridge.Cause, error)
}
```

- `session.Run` builds the intent from the selected service and passes it — no crypto
  import in the session:
  `BackendTLS{Enabled: selected.TLS, Verify: selected.TLSVerify}`.
- `realBridger.Bridge` translates intent → `*tls.Config`:
  - `Enabled == false` → `nil` (plaintext; calls `bridge.Bridge(..., nil)`).
  - `Enabled == true` →
    ```go
    host, _, _ := net.SplitHostPort(addr)
    cfg := &tls.Config{
        MinVersion:         tls.VersionTLS12, // matches the inbound listener floor
        ServerName:         host,             // always the configured host
        InsecureSkipVerify: !btls.Verify,     // RootCAs nil ⇒ system roots
    }
    ```
  - `ServerName` is set even when verify is off (harmless; also serves SNI).
- `fakeBridger` in `session_test.go` records the `BackendTLS` it receives.

### 4. Seed (`internal/seed`)

- `SeedService.Verify *bool` (`json:"verify"`). A **pointer** so an omitted field means
  `true` — the secure default holds even when the JSON is silent.
- `Apply` resolves `nil → true` and passes the bool to `CreateService`.

## Data flow

```
session.Run picks *store.Service (has .TLS, .TLSVerify, .Host, .Port)
  → Bridger.Bridge(conn, addr, termType, escapeAID,
                    BackendTLS{Enabled: svc.TLS, Verify: svc.TLSVerify})
    → realBridger builds *tls.Config (or nil) from intent + host
      → bridge.Bridge(conn, addr, termType, escapeAID, tlsCfg)
        → nil: net.DialTimeout   |   non-nil: tls.DialWithDialer
          → existing Telnet-aware relay (unchanged)
```

## Error handling

- TLS handshake / verification failure surfaces as a dial error → `bridge.Bridge` returns
  `(CauseError, err)`, identical to today's plaintext dial-failure path. `session.Run`
  already maps `CauseError` to a menu error message ("Could not connect to <name>") and
  loops back to the menu. No new error path.
- Never log cert contents or credentials; lifecycle logging follows the existing `log`
  conventions (service name + addr, as today).

## Testing (TDD, package by package)

- **store:** `migrate()` idempotent across two calls; `CreateService` persists `verify`;
  `ListServicesForGroups` returns `TLSVerify`; a fresh DB has the column.
- **seed:** `Verify` pointer — omitted → `true`, explicit `false` → `false`,
  explicit `true` → `true`.
- **bridge:** new TLS-backend test — generate a self-signed cert in-test, run a listener
  that speaks the minimal Telnet handshake like the existing fake backend, dial via
  `Bridge` with a `tls.Config` that trusts the cert (custom `RootCAs` + matching
  `ServerName`) → relays successfully (mirror `TestBridgeNegotiatesAndRelays`). Plus a
  verify-fail case: untrusted cert with `InsecureSkipVerify=false` → `CauseError`.
- **server (realBridger translation):** pure unit test of intent → `*tls.Config`:
  `Enabled=false → nil`; `Enabled=true,Verify=true → InsecureSkipVerify=false`;
  `Verify=false → InsecureSkipVerify=true`; `ServerName == host` in both enabled cases. No
  network.
- **server (session):** `fakeBridger` captures `BackendTLS`; assert a TLS service
  propagates `Enabled`/`Verify` correctly.
- Run `go test ./... -race` (the bridge is concurrent).

### Manual smoke test (protocol surface)

Per `CLAUDE.md`, the 3270/TLS protocol surface is only truly verified against a live
emulator + backend. Point a TLS service at a TLS-terminated TN3270 backend and confirm the
bridge works:
- `verify` **off** against a self-signed backend → connects and bridges.
- `verify` **on** against a backend whose cert matches its host and chains to a system
  root → connects; against an untrusted/self-signed host → fails closed back to the menu.
A real TLS TN3270 backend is required; note this as a checklist task in the plan (like the
MVP's Task 15).

## Documentation updates

- `internal/store` and `seed.example.json`: show a `verify` example service.
- `CLAUDE.md`: change the "reserved hook" note for backend TLS to "implemented"; add the
  per-service verify policy to conventions/gotchas as appropriate.
- `docs/superpowers/ROADMAP.md`: mark #2 done; update the "what's already wired" table.

## Out of scope (deferred)

- Custom CA bundle / private-CA trust file.
- Per-service ServerName override (connect-by-IP, verify-by-name).
- Any change to `tn3270proxy.json` — backend TLS is per-service (DB/seed), not a listener
  concern.
- Client-certificate (mutual TLS) auth to the backend.
- Cert reload / rotation.
