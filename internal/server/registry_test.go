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
	"sync"
	"testing"
	"time"
)

func TestRegistry_RegisterAssignsIncreasingIDs(t *testing.T) {
	r := newSessionRegistry()
	id1 := r.register("1.1.1.1:5000", time.Unix(100, 0), func() {})
	id2 := r.register("2.2.2.2:5000", time.Unix(200, 0), func() {})
	if id1 == 0 || id2 <= id1 {
		t.Fatalf("ids must be nonzero and increasing: id1=%d id2=%d", id1, id2)
	}
}

func TestRegistry_SnapshotReflectsEntriesSortedByID(t *testing.T) {
	r := newSessionRegistry()
	r.register("1.1.1.1:5000", time.Unix(100, 0), func() {})
	r.register("2.2.2.2:5000", time.Unix(200, 0), func() {})
	got := r.Snapshot()
	if len(got) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(got))
	}
	if got[0].ID >= got[1].ID {
		t.Errorf("snapshot not sorted by id: %+v", got)
	}
	if got[0].RemoteAddr != "1.1.1.1:5000" || !got[0].ConnectedAt.Equal(time.Unix(100, 0)) {
		t.Errorf("entry 0 = %+v, want addr/connectedAt populated", got[0])
	}
}

func TestRegistry_SnapshotReturnsCopies(t *testing.T) {
	r := newSessionRegistry()
	id := r.register("1.1.1.1:5000", time.Unix(100, 0), func() {})
	snap := r.Snapshot()
	snap[0].Username = "MUTATED" // mutate the returned copy
	r.setLogin(id, "REAL", time.Unix(150, 0))
	if again := r.Snapshot(); again[0].Username != "REAL" {
		t.Errorf("Snapshot must return copies; registry state leaked: %q", again[0].Username)
	}
}

func TestRegistry_DeregisterRemoves(t *testing.T) {
	r := newSessionRegistry()
	id := r.register("1.1.1.1:5000", time.Unix(100, 0), func() {})
	r.deregister(id)
	if got := r.Snapshot(); len(got) != 0 {
		t.Fatalf("snapshot len = %d after deregister, want 0", len(got))
	}
}

func TestRegistry_LoginAndServiceMutators(t *testing.T) {
	r := newSessionRegistry()
	id := r.register("1.1.1.1:5000", time.Unix(100, 0), func() {})

	r.setLogin(id, "ALICE", time.Unix(150, 0))
	v := r.Snapshot()[0]
	if v.Username != "ALICE" || !v.LoggedInAt.Equal(time.Unix(150, 0)) {
		t.Fatalf("after setLogin: %+v", v)
	}

	r.setService(id, "PROD")
	if v = r.Snapshot()[0]; v.Service != "PROD" {
		t.Fatalf("after setService: service = %q", v.Service)
	}

	r.clearService(id)
	if v = r.Snapshot()[0]; v.Service != "" {
		t.Fatalf("after clearService: service = %q", v.Service)
	}

	r.clearLogin(id)
	if v = r.Snapshot()[0]; v.Username != "" || !v.LoggedInAt.IsZero() {
		t.Fatalf("after clearLogin: %+v", v)
	}
}

func TestRegistry_MutatorsOnMissingIDAreNoOps(t *testing.T) {
	r := newSessionRegistry()
	r.setLogin(999, "X", time.Unix(1, 0))
	r.clearLogin(999)
	r.setService(999, "Y")
	r.clearService(999)
}

func TestRegistry_ConcurrentAccessIsRaceFree(t *testing.T) {
	r := newSessionRegistry()
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			id := r.register("9.9.9.9:1", time.Unix(1, 0), func() {})
			r.setLogin(id, "U", time.Unix(2, 0))
			r.setService(id, "S")
			_ = r.Snapshot()
			r.clearService(id)
			r.clearLogin(id)
			r.deregister(id)
		})
	}
	wg.Wait()
	if got := len(r.Snapshot()); got != 0 {
		t.Fatalf("all sessions deregistered, want 0, got %d", got)
	}
}

func TestRegistry_DisconnectClosesAndReturnsView(t *testing.T) {
	r := newSessionRegistry()
	closes := 0
	id := r.register("3.3.3.3:7000", time.Unix(100, 0), func() { closes++ })
	r.setLogin(id, "BOB", time.Unix(120, 0))

	booted, ok := r.Disconnect(id)
	if !ok {
		t.Fatal("Disconnect on a live session must return ok=true")
	}
	if closes != 1 {
		t.Errorf("close called %d times, want 1", closes)
	}
	if booted.Username != "BOB" || booted.RemoteAddr != "3.3.3.3:7000" {
		t.Errorf("booted view = %+v, want BOB@3.3.3.3:7000", booted)
	}
	// Disconnect must NOT deregister: the session goroutine's own teardown defer
	// removes the entry once its blocked Read unblocks. A second Disconnect (or
	// the audit lookup) must still find the entry until then.
	if snap := r.Snapshot(); len(snap) != 1 {
		t.Errorf("Disconnect must not remove the entry; Snapshot len = %d, want 1", len(snap))
	}
}

func TestRegistry_DisconnectMissingIDIsBenign(t *testing.T) {
	r := newSessionRegistry()
	_, ok := r.Disconnect(404)
	if ok {
		t.Error("Disconnect on an unknown id must return ok=false")
	}
}

func TestRegistry_SatisfiesSeam(t *testing.T) {
	var _ SessionRegistry = newSessionRegistry()
}
