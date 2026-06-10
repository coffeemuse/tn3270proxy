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

package main

import (
	"fmt"

	ver "github.com/coffeemuse/tn3270proxy/internal/version"
)

// version is the build-time version stamp. Override at link time with:
//
//	go build -ldflags "-X main.version=v1.2.3" -o bin/tn3270proxy ./cmd/tn3270proxy
//
// When not overridden, ver.Resolve falls back to VCS info from
// runtime/debug.ReadBuildInfo (short commit hash, +"-dirty" if modified).
var version = "dev"

// resolvedVersion returns the effective version for this process.
// Centralised here so the serve startup log and version subcommand agree.
func resolvedVersion() string {
	return ver.Resolve(version)
}

// runVersion prints the resolved version and returns.
func runVersion() {
	fmt.Println(resolvedVersion())
}
