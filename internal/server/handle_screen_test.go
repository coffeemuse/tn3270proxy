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

package server

import (
	"errors"
	"slices"
	"testing"

	"github.com/racingmars/go3270"
)

func TestHandleScreenSilentRePresent(t *testing.T) {
	for _, silent := range []go3270.AID{go3270.AIDPA1, go3270.AIDPA2, go3270.AIDPA3, go3270.AIDClear} {
		calls := 0
		resp, err := handleScreen(func() (go3270.Response, error) {
			calls++
			if calls == 1 {
				return go3270.Response{AID: silent}, nil
			}
			return go3270.Response{AID: go3270.AIDEnter}, nil
		})
		if err != nil {
			t.Errorf("AID 0x%02X: unexpected error: %v", silent, err)
		}
		if calls != 2 {
			t.Errorf("AID 0x%02X: want 2 calls (silent re-present), got %d", silent, calls)
		}
		if resp.AID != go3270.AIDEnter {
			t.Errorf("AID 0x%02X: want final AID Enter, got 0x%02X", silent, resp.AID)
		}
	}
}

func TestHandleScreenPassThrough(t *testing.T) {
	for _, aid := range []go3270.AID{go3270.AIDEnter, go3270.AIDPF3, go3270.AIDPF4} {
		calls := 0
		resp, err := handleScreen(func() (go3270.Response, error) {
			calls++
			return go3270.Response{AID: aid}, nil
		})
		if err != nil || calls != 1 || resp.AID != aid {
			t.Errorf("AID 0x%02X: want pass-through on first call: err=%v calls=%d aid=0x%02X",
				aid, err, calls, resp.AID)
		}
	}
}

func TestHandleScreenErrorPassThrough(t *testing.T) {
	want := errors.New("conn error")
	_, err := handleScreen(func() (go3270.Response, error) {
		return go3270.Response{}, want
	})
	if err != want {
		t.Errorf("want %v, got %v", want, err)
	}
}

func TestHandleScreenMultipleSilentsThenExit(t *testing.T) {
	silents := []go3270.AID{go3270.AIDPA3, go3270.AIDClear, go3270.AIDPA1}
	calls := 0
	resp, err := handleScreen(func() (go3270.Response, error) {
		calls++
		if calls <= len(silents) {
			return go3270.Response{AID: silents[calls-1]}, nil
		}
		return go3270.Response{AID: go3270.AIDPF3}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.AID != go3270.AIDPF3 {
		t.Errorf("want PF3, got 0x%02X", resp.AID)
	}
	if calls != len(silents)+1 {
		t.Errorf("want %d calls, got %d", len(silents)+1, calls)
	}
}

// TestWithSilentExits guards the actual bug: go3270.HandleScreenAlt only
// RETURNS PA1/PA2/PA3/Clear to the caller when they are exit keys. If the call
// sites pass a bare exitkeys slice, those AIDs fall into go3270's internal
// "unknown key" branch and handleScreen never sees them — so the silent
// re-present silently does nothing. Every silent AID must be in the result.
func TestWithSilentExits(t *testing.T) {
	base := []go3270.AID{go3270.AIDPF3, go3270.AIDPF4}
	got := withSilentExits(base)

	for _, want := range []go3270.AID{go3270.AIDPF3, go3270.AIDPF4,
		go3270.AIDPA1, go3270.AIDPA2, go3270.AIDPA3, go3270.AIDClear} {
		if !slices.Contains(got, want) {
			t.Errorf("withSilentExits result missing AID 0x%02X", want)
		}
	}

	// Every silent AID handleScreen loops on must be an exit key, or the loop
	// is unreachable against the real library.
	for _, s := range silentAIDList {
		if !slices.Contains(got, s) {
			t.Errorf("silent AID 0x%02X not in exit keys — handleScreen unreachable", s)
		}
	}

	// The caller's slice must not be mutated.
	if len(base) != 2 {
		t.Errorf("withSilentExits mutated caller slice: len=%d", len(base))
	}
}
