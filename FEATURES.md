# TN3270Proxy — Features at a Glance

**One secure front door to all your 3270 hosts.**

TN3270Proxy presents itself as a TN3270 server on your network, authenticates
every user, shows each user a **personalized, group-filtered menu** of the
internal mainframe and midrange services they're allowed to reach, and bridges
them straight through to the host they pick. Authentication, authorization,
auditing, and administration are **all built in** — and all driven from the
familiar green-screen 3270 interface your operators already know.

It is **100% original code** (only third-party Go libraries are reused) —
written from scratch, not forked. It takes its inspiration from the classic
[`proxy3270`](https://github.com/racingmars/proxy3270) forwarding service:
where `proxy3270` is a clean, static "pick a host and connect" relay,
TN3270Proxy is a full **access gateway**, built from the ground up for exposure
to untrusted networks.

---

## Why it exists

Mainframe and legacy 3270 services were never designed to sit on a hostile
network. Most shops solve this with a tangle of jump boxes, VPNs, and static
port forwards. TN3270Proxy collapses that into a **single hardened endpoint**:

- Users connect to **one** address.
- They prove who they are **before** they can see — let alone reach — anything.
- They only ever see the services their group grants them.
- Every connection, login, and host selection is **recorded**.
- Admins manage the whole thing from a 3270 terminal — no config files, no restarts, no dropped sessions.

---

## Headline features

### 🔐 Real user authentication
Per-user accounts with **bcrypt-hashed** passwords stored in an embedded
database. Login failures are deliberately uniform — no username-enumeration
leak. A first-run `bootstrap` command mints a one-time admin password from a
cryptographic RNG (never a hard-coded default).

### 👥 Group-based access control
Users belong to groups; services are granted to groups. Each user's menu is
**filtered to exactly what they're entitled to see** — backend hostnames and
ports are never exposed to ordinary users. A reserved `ZZADMIN` group unlocks
the in-band admin console.

### 🔑 Opt-in multi-factor authentication (TOTP)
Standards-based TOTP (RFC 6238) with **AES-256-GCM encryption of secrets at
rest**. Users self-enroll from a settings screen; enforcement is *secret-first*
(any enrolled secret is always verified). The MFA master key is infrastructure-
level (env/file, never the database), with a fail-closed startup check and a
break-glass `mfa reset-all` recovery path.

### 🖥️ Full in-application admin console — no config files
A complete **admin UI rendered as 3270 screens**, with seven areas reachable
from one menu: **Users**, **Groups**, and **Services** (full CRUD with paging,
line commands, and delete confirmations); **Sysparms** to edit runtime
parameters live (MOTD, login branding, system ID, auth-throttle tuning);
**Networks** to manage the trusted-network allow-list; **Audit** to browse the
trail from a green screen; and **Sessions** to watch active clients and
disconnect them in real time. Guardrails throughout (no self-delete, the admin
group can never be emptied, no self-demotion). **Every change takes effect
immediately. No restart. No dropped sessions.**

### 📡 Live session monitoring & control
Operators see **every active client session** from the admin console — who's
connected, from where, how long, and what state they're in — and can **disconnect
a session on the spot**. Nothing about the running system requires a restart to
inspect or intervene.

### ⚙️ Self-service for every user
A `0` menu entry gives each user a settings screen to **change their own
password** and **manage their own MFA** (enroll, re-enroll, or disable), gated
by a current-password step-up.

### 📓 Durable, queryable audit trail
Every connect, login success/failure, service bridge (with outcome), admin
change, and disconnect is written to a session-correlated **audit table** with
UTC timestamps. Browse it **right from the 3270 admin console**, query it from
the CLI with `audit list`, and age it out with `audit prune -older-than 90d`.
Credentials are *never* logged.

### 🛡️ Built to face a hostile network
- **Idle-timeout regimes** that change per phase: a strict pre-auth window *with
  a non-sliding absolute ceiling* (a slow trickle can't hold a slot open),
  a post-auth window that **logs back to the login screen** rather than
  dropping you, and a configurable bridge-idle policy.
- **Connection caps** — global and per-IP — with a **trusted-network allow-list**
  (CIDR-based) that bypasses the DoS controls for known-good networks. The list
  lives in the database and is managed live from the admin console, so changes
  apply to new connections without a restart.
- **Failed-auth throttling** — per-username linear backoff shared across the
  password *and* MFA paths, with decay and the applied delay recorded in the
  audit detail. (fail2ban-friendly.)
- Per-connection **panic recovery** so one bad session never takes down the server.

### 🔒 TLS on both legs
Immediate-TLS inbound listeners (the mode real 3270 emulators expect) **and**
per-service backend TLS with a per-service certificate-verification toggle
(browser-like system-root + hostname verification by default; an explicit
encrypt-without-verify mode for self-signed internal hosts).

### 📐 Modern 3270 polish
- An **ISPF-style three-band layout** across the admin and menu screens (centered
  title, command line, red message line, body, PF-key help) with a documented CUA
  palette and style guide. (Two deliberate exceptions: the branding-forward login
  screen and the chrome-less MOTD/NEWS pages.)
- A **branding-forward login screen** that renders your own custom green-screen
  art from a `BRANDING_FILE` — make the front door look like your shop.
- **Larger terminal models** — MOD 2/3/4/5 — render correctly, using the extra
  rows for longer menus.
- A right-hand **status block** (User ID / date / time / terminal / system ID /
  release) on the menu.
- **Paginated menus** with stable global numbering (PF7/PF8).
- Consistent navigation: **PF3** uniformly steps back one level; **PA3** returns
  you to the menu from inside a bridged host session.

### 📊 Structured logging & versioning
`slog`-based logging — human-readable text to stderr plus optional JSON to a
file, with selectable levels. Release builds stamp an exact version
(`-X main.version`), falling back to the VCS revision (with a `-dirty` marker)
otherwise.

### 📦 Modern packaging
Pure-Go, **no cgo**, embedded SQLite (no external database to run). Ships as a
multi-stage **distroless container image**, multi-arch images on GHCR, and
tagged releases with `SHA256SUMS`, plus a ready-to-run `docker-compose` example.

---

## Side-by-side comparison

| Capability | **TN3270Proxy** | proxy3270 |
|---|:---:|:---:|
| Pick-a-host menu + bridge | ✅ | ✅ |
| TLS inbound listener | ✅ | ✅ |
| Backend (server-side) TLS + cert-verify toggle | ✅ | ✅ |
| Paginated menu (PF7/PF8) | ✅ | ✅ |
| **User authentication** | ✅ bcrypt accounts | ❌ none — anyone who connects gets the menu |
| **Per-user / group-based menus** | ✅ | ❌ one flat menu for everyone |
| **Multi-factor auth (TOTP)** | ✅ encrypted at rest | ❌ |
| **In-app admin UI (users/groups/services)** | ✅ live 3270 CRUD | ❌ edit JSON + restart |
| **In-app runtime parameters (MOTD/branding/system ID/throttling)** | ✅ live 3270 Sysparms | ❌ |
| **In-app audit browser** | ✅ 3270 screen | ❌ |
| **Live session monitoring + disconnect** | ✅ 3270 Active Sessions | ❌ |
| **Live config changes (no restart, no dropped sessions)** | ✅ | ❌ restart drops all connections |
| **Self-service password / MFA** | ✅ | ❌ |
| **Durable audit trail (queryable)** | ✅ SQLite table + CLI | ❌ log lines only |
| **Failed-auth throttling / lockout** | ✅ per-user backoff | ❌ |
| **Connection caps (global + per-IP)** | ✅ | ❌ |
| **Idle timeouts / pre-auth ceiling** | ✅ phase-aware regimes | ❌ |
| **Trusted-network bypass list** | ✅ DB-backed, live-managed | ❌ |
| **Return to menu after host session** | ✅ PA3 + re-login | ⚠️ session ends (menu recovery unsolved upstream) |
| Embedded database (no external DB) | ✅ SQLite | ➖ JSON config file |
| Structured logging (slog, levels, JSON file) | ✅ | ➖ zerolog text, debug/trace |
| Larger terminal models (MOD 3/4/5) | ✅ | ✅ |
| ISPF-style layout + style guide | ✅ | ➖ minimal menu |
| Status block (user/date/terminal/release) | ✅ | ❌ |
| Login disclaimer / banner | ✅ MOTD/NEWS | ✅ 2-line disclaimer |
| Custom login branding art (`BRANDING_FILE`) | ✅ | ❌ |
| Version stamping | ✅ ldflags + VCS fallback | ❌ |
| Container image / multi-arch releases | ✅ distroless + GHCR | ❌ |
| Telnet "un-negotiation" handoff option | ➖ unnecessary by design¹ | ✅ flag |

✅ = full support · ⚠️ = partial / known limitation · ➖ = present but minimal · ❌ = not available

¹ *proxy3270 relays the two Telnet legs raw, so it offers an `-unnegotiate`
flag (and a tunable timeout) to untangle double-negotiation for stricter
clients like IBM PCOMM. TN3270Proxy's bridge is a **Telnet terminator on both
legs** — each side negotiates with the proxy independently and only 3270 data
crosses between them — so the double-negotiation that flag works around is
structurally impossible.*

---

## The bottom line

| | **TN3270Proxy** | proxy3270 |
|---|---|---|
| **What it is** | A 3270 *access gateway* | A static 3270 *forwarder* |
| **Who can connect** | Authenticated, authorized users only | Anyone on the network |
| **Who sees what** | Each user sees only their group's hosts | Everyone sees every host |
| **How you manage it** | Live 3270 admin console, zero downtime | Edit JSON, restart, drop everyone |
| **What you can prove** | A durable, queryable audit trail | What the logs happened to catch |
| **Designed for** | A public-facing edge | A trusted LAN |

proxy3270 is an excellent, focused tool for getting users in front of a list of
hosts. **TN3270Proxy takes that idea and makes it safe to put on the open
internet** — adding the identity, authorization, auditing, and operations layer
a real gateway needs, without ever leaving the 3270 screen your users already
live in.

---

## Third-party libraries

TN3270Proxy's own code is 100% original. It builds on a small set of
permissively licensed, pure-Go open-source libraries — none of them
copyleft, all compatible with redistribution.

| Library | Version | License | Used for |
|---|---|---|---|
| `github.com/racingmars/go3270` | v0.9.13 | MIT | 3270 screen control & Telnet negotiation |
| `modernc.org/sqlite` | v1.51.0 | BSD-3-Clause | Embedded, pure-Go SQLite (no cgo) |
| `golang.org/x/crypto` | v0.52.0 | BSD-3-Clause | bcrypt password hashing |
| `github.com/pquerna/otp` | v1.5.0 | Apache-2.0 | TOTP (RFC 6238) multi-factor auth |

*These direct dependencies in turn pull in a handful of transitive
dependencies, all under the same permissive MIT / BSD-3-Clause / Apache-2.0
terms.*

---

*TN3270Proxy is distributed under the GNU General Public License v3.*
