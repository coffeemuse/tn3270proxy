# TN3270Proxy — TLS-terminated inbound listener (design)

**Status:** approved (brainstorm complete) — ready for implementation plan.
**Roadmap item:** #1 (TLS-terminated inbound listener). See `docs/superpowers/ROADMAP.md`.
**Date:** 2026-06-03

## Goal

Let the proxy accept **TLS** connections from TN3270 clients (TN3270 over TLS, a.k.a.
"TN3270 Secure"), not just plaintext. This is the gating item for any real public exposure.

The administrator decides which transports are live: the proxy can run a **plaintext**
listener, a **TLS** listener, or **both at once**, each on its own address. Some internal
backends/clients can't do TLS, so plaintext must remain a first-class, admin-selectable
option — not a deprecated fallback.

## Scope

In scope:
- A JSON **config file** describing the two listeners (plain + TLS), certs, and the DB path.
- TLS-terminated inbound listener using immediate TLS (not STARTTLS).
- Running both listeners simultaneously, sharing one session handler.
- Backward compatibility: existing flag-based invocation keeps working unchanged.

Out of scope (YAGNI for v1 — noted so they aren't silently dropped):
- **Cert hot-reload.** Certs load once at startup; changing them requires a restart.
- **mTLS / client-certificate auth.** Additive later via `tls.Config.ClientAuth`.
- **Per-listener distinct handlers or config.** Both listeners share one handler.
- Backend-side TLS dialing — that is roadmap item #2, a separate spec.

## Configuration

### File format and location

Format is **JSON** (stdlib `encoding/json`, no new dependency, consistent with the existing
seed file; keeps the project pure-Go / no-cgo).

Default path: **`tn3270proxy.json`** in the working directory (mirrors the `tn3270proxy.db`
default-naming convention).

```json
{
  "db": "tn3270proxy.db",
  "listeners": {
    "plain": { "enabled": true, "addr": ":2323" },
    "tls": {
      "enabled": true,
      "addr": ":3270",
      "cert": "/etc/tn3270proxy/server.crt",
      "key": "/etc/tn3270proxy/server.key"
    }
  }
}
```

Two **named, independent** listeners — `plain` and `tls`. Named (rather than a list) because
there are exactly two transport kinds; it keeps the JSON and validation simple. Each has its
own `enabled` and `addr`. `tls` additionally carries `cert` and `key` file paths.

### Loading precedence

`config.Load` builds the effective config as **defaults < config file < explicit flags**:

1. **Defaults:** `plain` enabled at `:2323`, `tls` disabled, `db = tn3270proxy.db`.
   With no config file and no flags, this is **identical to today's behavior** (plaintext on
   `:2323`) — fully backward compatible.
2. **Config file:** merged on top of defaults.
   - If `-config <path>` is given explicitly: read it; a missing/unreadable/invalid file is a
     **fatal error**.
   - If `-config` is omitted: look for `tn3270proxy.json` in the working dir. If present, load
     it. If absent, silently keep defaults (**not** an error).
3. **Explicit flags:** only flags the user actually set on the command line override the merged
   result (detected via `flag.FlagSet.Visit`, which visits only set flags):
   - `-db` overrides `db`.
   - `-listen` overrides `listeners.plain.addr`.
   - `-config` selects the file (step 2).

   This preserves the current `serve -listen ... -db ...` ergonomics.

### Validation (in `config.Load`, returns `error`)

- If `tls.enabled`: `cert` and `key` are required, and the pair must load successfully via
  `tls.LoadX509KeyPair` at startup (fail fast on a bad/missing cert).
- If **no** listener is enabled: error (`nothing to serve`).
- If `-config` is given explicitly but the file is missing/unreadable/invalid JSON: error.

## Architecture

### `internal/config`

`Config` grows from the current flat `{ListenAddr, DBPath}` into a structure carrying both
listeners. Suggested shape (final names settled during implementation):

```go
type ListenerConfig struct {
    Enabled bool
    Addr    string
}

type TLSListenerConfig struct {
    ListenerConfig
    Cert string
    Key  string
}

type Config struct {
    DBPath string
    Plain  ListenerConfig
    TLS    TLSListenerConfig
}
```

`Load(args []string) (Config, error)` parses flags, applies the precedence above, validates,
and returns the effective `Config`. The JSON file unmarshals into a parallel
`(field-tagged)` struct that is then merged onto defaults.

### Listener construction

A helper turns the validated `Config` into the set of live listeners:

```go
// returns one net.Listener per enabled transport.
func BuildListeners(cfg Config) ([]net.Listener, error)
```

- `plain.enabled` → `net.Listen("tcp", cfg.Plain.Addr)`.
- `tls.enabled` → `net.Listen("tcp", cfg.TLS.Addr)` wrapped in `tls.NewListener(ln, tlsCfg)`.
  - TN3270 clients expect **immediate TLS** on connect (not STARTTLS); `tls.NewListener`
    does exactly that.
  - `tlsCfg`: `Certificates` from `LoadX509KeyPair(cfg.TLS.Cert, cfg.TLS.Key)`,
    `MinVersion: tls.VersionTLS12`.

Because a `*tls.Conn` is a `net.Conn`, **nothing downstream changes**: `server.Session`,
go3270's `NegotiateTelnet` / `HandleScreen`, and the bridge all operate on `net.Conn`.

Where this helper lives (a small `internal/listen` package, `internal/server`, or `main`) is
an implementation detail to settle in the plan; it must be unit-testable in isolation.

### Multi-listener serving

`server.Server` **stays single-listener and unchanged** — its accept loop is already tested
and is a clean single responsibility. A small orchestrator runs one accept loop per listener:

```go
// runs one Server.Serve per listener, all sharing the same handler.
// first listener error closes the rest and returns.
func ServeAll(listeners []net.Listener, handler connHandler) error
```

- One goroutine per listener, each wrapping the existing `Server{Listener, Handler}.Serve()`.
- All share the **same** `Handler` (one `sessionHandler`, one `*store.Store`).
- On the first listener error, close the remaining listeners (unblocking their `Accept`) and
  return that error, so a single failure brings the process down cleanly rather than silently
  losing a transport.

### Wiring (`cmd/tn3270proxy`)

`runServe` becomes: `config.Load` → `store.Open` → `BuildListeners` → log which transports are
live → `ServeAll(listeners, NewSessionHandler(...))`. The current single `net.Listen` +
`Server.Serve` call is replaced by this path.

## Testing

Test-first, per repo convention. Run `go test ./... -race`.

**`internal/config`:**
- Precedence table tests: defaults only; file overrides defaults; explicit flag overrides
  file; `-listen`/`-db` override the right fields; unset flags do **not** override.
- Default-file pickup: `tn3270proxy.json` present in CWD is loaded; absent → defaults, no error.
- Validation errors: `tls.enabled` without cert/key; no listener enabled; explicit `-config`
  pointing at a missing/invalid file.

**TLS listener:**
- Generate an **ephemeral self-signed cert** in `t.TempDir()` (in-process, `crypto/x509` +
  `crypto/tls`), start the TLS listener, dial it with a `crypto/tls` client, and confirm a
  connection is accepted and go3270 negotiation completes over it. Mirrors the existing bridge
  negotiation test style. Hermetic — does not depend on any on-disk cert.

**`ServeAll`:**
- Both listeners up: a client on each reaches the handler.
- One listener failing tears the rest down and returns the error.

**Manual smoke test (real emulator):** a self-signed `server.crt` / `server.key` placed in the
repo root (gitignored) for `c3270`/TLS-capable emulator testing against the TLS port. Provide a
regeneration one-liner (e.g. `openssl req -x509 ...` or a tiny `go run` generator). The 3270
protocol surface (screens, cursor, negotiation, PA3) is only truly verified against a live
emulator — see the smoke-test checklist in the MVP plan.

## Repo housekeeping

- Add `server.crt`, `server.key`, and `*.crt` / `*.key` (and `tn3270proxy.json` if it should be
  per-deployment) to `.gitignore` so local test certs and configs aren't committed.
- Update `README.md` with the config-file format and a TLS quick-start.
- Update `CLAUDE.md` config notes once the shape lands.

## Backward compatibility

- No config file + no new flags → plaintext on `:2323`, exactly as today.
- `serve -listen :2323 -db proxy.db` keeps working (flags override).
- `tn3270proxy.json` in CWD is auto-discovered, enabling TLS without changing the command line.
