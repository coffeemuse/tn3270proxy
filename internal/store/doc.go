/*
 * Copyright 2026 by CoffeeMuse.
 *
 * This file is part of tn3270proxy.
 *
 * tn3270proxy is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * tn3270proxy is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with tn3270proxy. If not, see <https://www.gnu.org/licenses/>.
 */

// Package store owns ALL SQL in the gateway: users, groups, services,
// trusted networks, runtime system parameters, MFA state, and the audit
// trail, in one SQLite database (modernc.org/sqlite — pure Go, no cgo). No
// other package writes SQL; callers depend on narrow interface slices of
// *Store (auth.UserStore, server.AdminStore, …).
//
// Two cross-cutting contracts live at this layer:
//
//   - Canonical UPPERCASE names. Usernames, group names, and service NAMEs
//     fold to upper case here — the single choke point — and the UNIQUE
//     columns are COLLATE NOCASE, so Go-side case-sensitive compares
//     (e.g. slices.Contains(groups, store.AdminGroup)) are correct as
//     written. Passwords and service hosts are never normalized.
//
//   - Versioned, forward-only schema migration (migrate.go). Structural
//     changes append to the migrations ledger (never edit shipped steps);
//     PRAGMA user_version stamps progress; Open refuses a newer-than-binary
//     DB and writes a VACUUM INTO backup before migrating. Code-defined
//     default rows (the ZZADMIN group, sysconfig.Catalog seeds) are applied
//     by reconcileDefaults on every Open, outside the ledger, so new
//     defaults reach already-migrated databases.
//
// The store is mechanical by design: policy (admin guardrails, reserved
// group rules, duplicate-name messages) lives in internal/server's adminFlow.
package store
