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

// Package version resolves the effective version string for the binary.
// The primary source is a value injected at link time via:
//
//	go build -ldflags "-X main.version=v1.2.3"
//
// When the ldflags value is absent (equal to the dev sentinel), Resolve falls
// back to VCS metadata from runtime/debug.ReadBuildInfo, returning a short
// commit hash with an optional "-dirty" suffix.
package version

import "runtime/debug"

const devSentinel = "dev"

// Resolve returns the effective version. injected is the package-level var from
// cmd/tn3270proxy, set to the dev sentinel at compile time and overridden by
// ldflags for release builds. When injected equals the sentinel, Resolve falls
// back to VCS info from ReadBuildInfo.
func Resolve(injected string) string {
	return resolve(injected, debug.ReadBuildInfo)
}

// resolve is the testable core; readInfo is injectable for tests.
func resolve(injected string, readInfo func() (*debug.BuildInfo, bool)) string {
	if injected != devSentinel {
		return injected
	}
	info, ok := readInfo()
	if !ok {
		return devSentinel
	}
	var rev string
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if rev == "" {
		return devSentinel
	}
	short := rev
	if len(short) > 12 {
		short = short[:12]
	}
	if modified {
		return short + "-dirty"
	}
	return short
}
