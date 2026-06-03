# TN3270 Gateway — MVP Design

**Date:** 2026-06-03
**Status:** Approved for spec; pending user review of this document
**Scope:** Minimum viable product — the core connect → login → menu → bridge loop.

## 1. Purpose

A TN3270 gateway that presents itself as a TN3270 *server* on the public internet,
authenticates users, and brokers outbound TN3270 connections to internal ("backend")
services. The user never connects directly to internal hosts; the proxy mediates every
session and only exposes services the user's group memberships permit.

This document covers the **MVP** only. TLS, an admin management UI, TN3270E, audit
logging, and session multiplexing are explicitly deferred (see §9).

## 2. Core Loop (functional requirements)

A user points a 3270 emulator at the proxy's public address. The proxy then:

1. Accepts the inbound TCP connection and performs Telnet/TN3270 negotiation
   (via the `go3270` library).
2. Renders a **login screen**: username field + masked password field.
3. Authenticates the supplied credentials against the local database and loads the
   authenticated user's **group memberships**.
4. Renders a **service menu** listing only the services granted to the user's groups.
5. When the user selects a service, the proxy opens a **TN3270 client connection** to
   that backend host:port and **bridges** the two connections byte-for-byte.
6. When the backend session ends — backend closes, the connection drops, or the user
   presses the designated **escape AID** — the proxy tears down the backend connection
   and returns the user to the service menu (still authenticated).
7. The user may select another service, or disconnect.

### Acceptance criteria (MVP "done")

- A client can connect, log in with seeded credentials, and see a menu filtered by group.
- Invalid credentials re-prompt without revealing whether the username exists.
- Selecting a permitted service bridges to a (test) backend and the user can interact
  with it as if connected directly.
- Pressing the escape AID returns the user to the menu without dropping their proxy
  session.
- An unreachable/failed backend shows an error on the menu and keeps the user logged in.

## 3. Architecture

Standard Go project layout:

```
cmd/tn3270proxy/main.go   — entrypoint: load config, open DB, start listener
internal/config/          — configuration (listen address, DB path)
internal/server/          — accept loop + per-connection session state machine
internal/auth/            — Authenticate(username, password) → (Identity, error)
internal/store/           — DB access layer: users, groups, services (SQLite)
internal/screens/         — go3270 screen definitions (login, menu, error lines)
internal/bridge/          — backend TN3270 client dial + bidirectional relay
```

### Session state machine

Each accepted connection is driven by a per-connection state machine in
`internal/server`:

```
Negotiating → Login → Menu → Bridging → Menu → … → (Closed)
```

- **Negotiating**: `go3270` Telnet negotiation. Records the negotiated terminal type
  for later reuse on the backend leg.
- **Login**: render login screen; on submit, call `auth.Authenticate`. On success →
  Menu. On failure → re-render Login.
- **Menu**: query services visible to the identity's groups; render menu. On valid
  selection → Bridging. On quit → Closed.
- **Bridging**: hand the connection to `internal/bridge`. On return (any teardown
  cause) → Menu.

### Component responsibilities & interfaces

- **`store`**: owns all DB access. Exposes typed methods, e.g.
  `GetUserByUsername`, `GetUserGroups`, `ListServicesForGroups`. No SQL leaks outside
  this package.
- **`auth`**: depends on `store`. Verifies password hash, returns an `Identity`
  (user id, username, group names). Does not touch the network or 3270.
- **`screens`**: pure rendering — builds `go3270.Screen`/`Field` values from data
  passed in (e.g. a list of service names). No DB or network access.
- **`bridge`**: given a backend host:port and the client connection, dials the backend,
  negotiates the client leg of Telnet, and relays bytes until teardown. Returns the
  teardown cause to the server.
- **`server`**: orchestrates the above; owns the state machine and connection lifecycle.

## 4. Data Store

**Engine:** SQLite via `modernc.org/sqlite` (pure-Go driver, no cgo) so the binary
stays statically linkable and easy to cross-compile.

**Password hashing:** `golang.org/x/crypto/bcrypt`.

### Schema

```sql
users (
  id            INTEGER PRIMARY KEY,
  username      TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL
)

groups (
  id   INTEGER PRIMARY KEY,
  name TEXT UNIQUE NOT NULL
)

user_groups (
  user_id  INTEGER NOT NULL REFERENCES users(id),
  group_id INTEGER NOT NULL REFERENCES groups(id),
  PRIMARY KEY (user_id, group_id)
)

services (
  id   INTEGER PRIMARY KEY,
  name TEXT UNIQUE NOT NULL,   -- label shown on the menu
  host TEXT NOT NULL,
  port INTEGER NOT NULL,
  tls  INTEGER NOT NULL DEFAULT 0   -- reserved; backend-TLS dialing is a follow-on
)

group_services (
  group_id   INTEGER NOT NULL REFERENCES groups(id),
  service_id INTEGER NOT NULL REFERENCES services(id),
  PRIMARY KEY (group_id, service_id)
)
```

A service is visible/usable to a user if any of the user's groups is linked to it via
`group_services`.

### Seeding (MVP)

No admin UI in the MVP. The binary provides a `seed` subcommand (or equivalent small
CLI) to create users, groups, services, and their links, so a working environment can
be stood up for testing and demonstration. The schema is created on first run
(idempotent migration).

## 5. The Bridge

The bridge is the bespoke, highest-risk component (`go3270` is server-only and provides
no client side).

After a menu selection:

1. **Dial** the backend `host:port` (plain TCP for the MVP; the `tls` column is
   reserved for a follow-on).
2. **Negotiate the client leg of Telnet** with the backend: BINARY, END-OF-RECORD (EOR),
   SUPPRESS-GO-AHEAD, and TERMINAL-TYPE. The terminal type offered to the backend echoes
   the type the client negotiated with us in the Negotiating state, so screen geometry
   matches end-to-end.
3. **Relay**: two goroutines copy bytes in each direction (client↔backend) until either
   side closes or the escape condition fires.
4. **Escape**: the client→backend direction is watched for a designated **escape AID**
   (a chosen PF key, e.g. PF12 — exact key fixed during implementation). On detection,
   the bridge stops relaying, closes the backend connection, and returns control to the
   server with a "user escaped" cause.

During bridging the proxy performs **pure byte relay** — it does **not** parse or
rewrite the 3270 datastream (apart from the minimal inspection needed to detect the
escape AID). This keeps the MVP small and maximizes protocol fidelity.

**Teardown causes** returned to the server: `backend-closed`, `client-closed`,
`user-escaped`, `error`. All of them route back to the Menu state except
`client-closed`, which closes the proxy session.

## 6. Configuration

Minimal for the MVP, sourced from flags and/or a small config file:

- Listen address (e.g. `:2323`).
- Database file path.
- Escape AID key (default fixed in code; configurable later).

## 7. Error Handling

- **Auth failure**: re-render the login screen with a generic message; never disclose
  whether the username exists.
- **Backend unreachable / dial error / mid-session drop**: return to the menu with an
  error line; the proxy session stays alive and authenticated.
- **Negotiation failure on the client (inbound) leg**: log and close the connection.
- **Unexpected panics in a session goroutine**: recovered at the per-connection
  boundary so one session cannot crash the listener.

## 8. Testing Strategy

- **`auth`**: unit tests against a real temporary SQLite DB — correct password,
  wrong password, unknown user, group loading.
- **`store`**: CRUD + the group→services visibility query, against a temp DB.
- **`bridge`**: a fake backend TCP server (in-process) to verify bidirectional relay,
  clean teardown on each cause, and escape-AID detection.
- **`screens`**: table tests asserting field positions/attributes and that the menu
  renders exactly the permitted services.
- **`server`**: a state-machine test driving the transitions with fakes for `auth`
  and `bridge`.

## 9. Out of Scope (follow-on milestones)

- TLS-terminated inbound listener (public-facing hardening).
- Backend-side TLS dialing (the `services.tls` column is reserved for this).
- Admin management UI for users / groups / services.
- TN3270E.
- Audit logging.
- Connection pooling / session multiplexing / load balancing.
