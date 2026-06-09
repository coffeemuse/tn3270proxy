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
