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

package version

import (
	"runtime/debug"
	"testing"
)

func noInfo() (*debug.BuildInfo, bool) { return nil, false }

func buildInfo(settings ...debug.BuildSetting) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Settings: settings}, true
	}
}

func TestResolve(t *testing.T) {
	t.Run("uses injected when not dev sentinel", func(t *testing.T) {
		got := resolve("v1.2.3", noInfo)
		if got != "v1.2.3" {
			t.Errorf("got %q, want %q", got, "v1.2.3")
		}
	})

	t.Run("uses injected empty string (explicit override)", func(t *testing.T) {
		got := resolve("", noInfo)
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("falls back to dev when ReadBuildInfo unavailable", func(t *testing.T) {
		got := resolve("dev", noInfo)
		if got != "dev" {
			t.Errorf("got %q, want %q", got, "dev")
		}
	})

	t.Run("returns short VCS revision (truncates at 12)", func(t *testing.T) {
		got := resolve("dev", buildInfo(
			debug.BuildSetting{Key: "vcs.revision", Value: "abc123def456789"},
			debug.BuildSetting{Key: "vcs.modified", Value: "false"},
		))
		if got != "abc123def456" {
			t.Errorf("got %q, want %q", got, "abc123def456")
		}
	})

	t.Run("appends -dirty when working tree is modified", func(t *testing.T) {
		got := resolve("dev", buildInfo(
			debug.BuildSetting{Key: "vcs.revision", Value: "abc123def456789"},
			debug.BuildSetting{Key: "vcs.modified", Value: "true"},
		))
		if got != "abc123def456-dirty" {
			t.Errorf("got %q, want %q", got, "abc123def456-dirty")
		}
	})

	t.Run("returns dev when VCS revision absent", func(t *testing.T) {
		got := resolve("dev", buildInfo(
			debug.BuildSetting{Key: "vcs.modified", Value: "true"},
		))
		if got != "dev" {
			t.Errorf("got %q, want %q", got, "dev")
		}
	})

	t.Run("short revision unchanged when at most 12 chars", func(t *testing.T) {
		got := resolve("dev", buildInfo(
			debug.BuildSetting{Key: "vcs.revision", Value: "abc123"},
			debug.BuildSetting{Key: "vcs.modified", Value: "false"},
		))
		if got != "abc123" {
			t.Errorf("got %q, want %q", got, "abc123")
		}
	})

	t.Run("exactly 12 chars not truncated", func(t *testing.T) {
		got := resolve("dev", buildInfo(
			debug.BuildSetting{Key: "vcs.revision", Value: "abcdef012345"},
			debug.BuildSetting{Key: "vcs.modified", Value: "false"},
		))
		if got != "abcdef012345" {
			t.Errorf("got %q, want %q", got, "abcdef012345")
		}
	})
}
