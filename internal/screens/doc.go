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

// Package screens builds the go3270 screens the gateway renders to clients.
// It is pure rendering: builders take values in and return a go3270.Screen,
// validation Rules, and an initial Cursor — no database, no network, no
// clock reads (paint-time values like MenuStatus.Now are passed in, keeping
// builders deterministic and unit-testable).
//
// Layout follows the three-band ISPF convention specified in
// docs/ispf-style-guide.md: centered title on row 0, command line on row 1,
// red message line on row 2, body from row 3, PF-key help on the last row.
// All rows are 0-based and computed from a Geometry (taller MOD 3/4/5
// terminals get more body rows; content always stays within columns 0–79).
// Two screens deviate deliberately: the login screen (branding-forward
// layout, style guide §6.8) and the MOTD/NEWS screen (chrome-less classic
// TSO/READY look — no title, no PF help, a "***" page gate on the last row).
//
// A 3270 field's Col addresses its attribute byte, so input begins at Col+1;
// cursorAt (cursor.go) is the single home of that rule, and every builder
// returns its cursor through it. Unit tests assert field names, content, and
// color — not row numbers; positioning is verified against a real emulator
// (see the s3270-smoke-testing skill).
package screens
