# Design: structured logging (slog) — levels, dual-stream output, injected logger

**Issue:** [#52](https://github.com/coffeemuse/tn3270proxy/issues/52)
**Date:** 2026-06-05
**Status:** Approved design, pre-implementation
**Closes:** #52 · **Foundation for:** #51 (fail2ban auth logging), #22 (subneg-overflow log line)

## Problem

The proxy logs through the stdlib `log` package today — **15 `log.*` call sites** across
6 files (`cmd/tn3270proxy/bootstrap.go`, `internal/server/{server,session,auditor,admin,limiter}.go`),
with no level control and no structured fields. A public-facing gateway needs adjustable
verbosity for incident triage without a redeploy, and parseable, stably-keyed lines so a
fail2ban-style log scraper (#51) can act on auth failures. Neither is possible with the
current global, unleveled, free-text logging.

This issue lays the **structured-logging foundation**. It deliberately does not implement
the fail2ban line shape (#51) or the subnegotiation-overflow log line (#22) — those layer
on top of the field vocabulary and injected logger established here.

## Decisions (from brainstorming)

- **Injected `*slog.Logger`**, threaded through the existing seams (not a package global),
  matching the `Presenter`/`Bridger`/`Authenticator` DI-for-testability convention. Enables
  per-connection child loggers tagged with `remote`/`user`.
- **Single `log_level` string** (`error|warn|info|debug`), default `info`; config key + flag,
  following the existing defaults < file < flags precedence.
- **No custom TRACE level** and **no go3270-library tracing toggle** — both dropped as YAGNI.
  This also removes any level that would dump session bytes, shrinking the credential-safety
  surface.
- **Dual-stream output:** human-readable **text → stderr (always)**; machine-readable
  **JSON → file (when `log_file` is set)**. Both honor the same level.
- **Optional `log_file`**, written *in addition to* the console (not instead of). Log
  rotation is an operator concern (logrotate/journald), out of scope.

## Architecture

### New package: `internal/logging`

Pure handler construction (no app logic), mirroring how `screens` is pure rendering.

- **`ParseLevel(s string) (slog.Level, error)`** — case-insensitive map of
  `error|warn|info|debug` to the slog levels; any other value is an error so config
  validation can reject it (consistent with how `bridge_idle` rejects bad values).

- **`multiHandler`** — slog ships no fan-out handler, so we define one:
  - holds `[]slog.Handler`
  - `Enabled(ctx, lvl)` = OR over children
  - `Handle(ctx, rec)` = dispatch to each child whose `Enabled` is true; first error wins,
    remaining children still attempted
  - `WithAttrs` / `WithGroup` = return a `multiHandler` whose children are the mapped children
    (so `logger.With(...)` propagates to both streams)

- **`New(level slog.Level, file string) (*slog.Logger, io.Closer, error)`** — builds:
  - **console:** `slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})` — always
  - **file:** when `file != ""`, open it (`O_CREATE|O_WRONLY|O_APPEND`, 0o640) and add
    `slog.NewJSONHandler(f, &slog.HandlerOptions{Level: level})`
  - wrap in `multiHandler` when both present; otherwise just the text handler
  - return `slog.New(handler)`, an `io.Closer` for the file (a no-op closer when no file),
    and any open error.

### Config & flags (`internal/config`)

Following the existing `fileConfig` + `set[]` precedence pattern exactly:

- `Config` gains:
  ```go
  type Log struct {
      Level string // error|warn|info|debug
      File  string // "" = console only
  }
  ```
  on `Config.Log`.
- `fileConfig` gains:
  ```go
  Log *struct {
      Level *string `json:"level"`
      File  *string `json:"file"`
  } `json:"log"`
  ```
- Flags: `-log-level` (string), `-log-file` (string), merged via the `set[]` map.
- Defaults: `Level: "info"`, `File: ""`.
- `validate()` calls `logging.ParseLevel(cfg.Log.Level)` and returns a fatal config error on
  failure, e.g. `config: log.level: %q (want "error", "warn", "info", or "debug")`.

### Wiring & injection

- `runServe` builds the logger immediately after `config.Load`:
  ```go
  lvl, _ := logging.ParseLevel(cfg.Log.Level) // already validated in Load
  logger, closer, err := logging.New(lvl, cfg.Log.File)
  if err != nil { return err }
  defer closer.Close()
  ```
- **`NewSessionHandler(st, escapeAID, limits, logger)`** — new trailing `logger *slog.Logger`
  parameter. This is the injection seam.
- **Per-connection child logger:** at accept, derive `connLog := logger.With("remote", conn.RemoteAddr().String())`; after successful authentication, derive `sessLog := connLog.With("user", username)`. Every line emitted within a session is auto-tagged.
- The 15 existing `log.*` call sites convert to leveled, structured calls
  (`logger.Info/Warn/Debug` with keyed attrs). The startup "listening on …" banners and
  `warnIfNoAdmin` also become structured `Info`/`Warn` lines.
- **One-shot subcommands** (`bootstrap`'s single `log.Printf`) use a minimal default text
  logger to stderr — they have no session context and don't need the configurable path.

### Field vocabulary (the operator contract)

A small, stable set of attribute keys so #51/#22 layer on without renaming:

| Key       | Meaning                                              |
|-----------|------------------------------------------------------|
| `remote`  | client IP:port (`conn.RemoteAddr().String()`)        |
| `user`    | username (post-auth; never the password)             |
| `event`   | lifecycle event name (e.g. `accept`, `login`, `logout`, `bridge`) |
| `outcome` | result where applicable (e.g. `success`, `invalid-credentials`) |
| `error`   | error string (via `slog.Any("error", err)`)          |
| `trusted` | bool — whether the client is in `trusted_cidrs`      |

These keys must stay stable; #51 will pin its failregex/fields to them.

## Credential safety (preserved + tested)

The no-credential-logging rule is unchanged: passwords, `Login()` contents, and any future
TOTP secret are **never** logged at any level; usernames are fine. A unit test asserts that an
auth-failure log line contains the `user` field but not the submitted password string.

## Testing (TDD)

- **`internal/logging`:**
  - `ParseLevel` — each valid level + an invalid one (error).
  - `multiHandler` — a record reaches both children; `Enabled` is the OR; level filtering drops
    sub-threshold records on both streams; `WithAttrs` propagates to both.
  - `New` with a temp file — a logged line appears in the file (JSON) and the captured stderr
    (text); `New` with `file == ""` produces a working logger and a no-op closer.
- **`internal/config`:** `log.level`/`log.file` parse from JSON; flag overrides file; bad level
  is a fatal load error.
- **`internal/server`:** inject a logger backed by a `bytes.Buffer` (text handler) and assert
  key events emit with the expected fields; the no-password assertion above.
- `go test ./... -race` green.

## Scope

**In scope:** the `logging` package, config + flags, logger injection through
`NewSessionHandler`, per-connection child loggers, migration of all 15 existing call sites,
credential-safety test.

**Out of scope (explicit):**
- TRACE / byte-dump level (dropped).
- go3270-library tracing toggle (dropped).
- Log rotation (operator concern).
- The fail2ban auth-failure line shape (#51) and the subneg-overflow line (#22) — this issue
  only establishes the foundation (injected logger + stable field keys) they build on.
