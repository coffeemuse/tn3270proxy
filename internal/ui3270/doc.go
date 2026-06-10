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

// Package ui3270 is the generic 3270 screen-widget engine: paginated
// line-command lists (RunList), labeled-input forms (RunForm), read-only
// snapshot lists (RunSnapshotList), and detail records (RunDetail). The admin
// CRUD screens and the self-service user-settings flow are all built from
// these four drivers, so paging math, confirm-gating, and PF-key handling
// live in exactly one place.
//
// Each driver owns control flow (paging, the two-press confirm dance,
// command dispatch) and delegates painting to the Renderer seam: the
// production implementation wraps go3270 (NewGo3270Renderer); tests inject a
// scripted fake, so every driver is unit-testable without a terminal.
// Callers supply data and behavior through the *Config types — the driver
// never inspects domain payloads (the generic Item flows through untouched).
//
// Layout follows the same three-band ISPF convention as internal/screens
// (docs/ispf-style-guide.md), with row math in layout.go adapting to taller
// terminals (rows only; content stays within columns 0–79).
package ui3270
