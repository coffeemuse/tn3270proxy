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
	"sort"
	"sync"
	"time"
)

// SessionView is a point-in-time, copied snapshot of one live session. It holds
// no net.Conn and no close func — only display/audit data, safe to hand to the
// admin screen layer.
type SessionView struct {
	ID          uint64
	RemoteAddr  string
	ConnectedAt time.Time
	LoggedInAt  time.Time // zero ⇒ not logged in
	Username    string
	Service     string
}

// sessionEntry is the registry's live record for one connection.
type sessionEntry struct {
	view  SessionView
	close func() // hard-closes the connection; idempotent (wrapped in sync.Once by the handler)
}

// sessionRegistry tracks every live connection. One instance is created per
// process (NewSessionHandler) and shared across all listeners, mirroring how
// authThrottle is wired. All methods are safe for concurrent use.
type sessionRegistry struct {
	mu     sync.Mutex
	nextID uint64
	byID   map[uint64]*sessionEntry
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{byID: make(map[uint64]*sessionEntry)}
}

// register adds a live session and returns its id. close hard-closes the
// connection when called (the caller wraps it in sync.Once for idempotency).
func (r *sessionRegistry) register(remoteAddr string, connectedAt time.Time, close func()) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	id := r.nextID
	r.byID[id] = &sessionEntry{
		view:  SessionView{ID: id, RemoteAddr: remoteAddr, ConnectedAt: connectedAt},
		close: close,
	}
	return id
}

// deregister removes a session (called from the session's teardown defer).
func (r *sessionRegistry) deregister(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byID, id)
}

// Snapshot returns a copied, id-sorted slice of the live sessions. The copies
// never alias the live entries, so the caller can format them freely.
func (r *sessionRegistry) Snapshot() []SessionView {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SessionView, 0, len(r.byID))
	for _, e := range r.byID {
		out = append(out, e.view) // value copy
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// setLogin records a successful login on the entry (username + login time).
func (r *sessionRegistry) setLogin(id uint64, username string, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.byID[id]; e != nil {
		e.view.Username = username
		e.view.LoggedInAt = at
	}
}

// clearLogin reverts the entry to a pre-auth state (logoff / idle logout). It
// also clears any service, defensively — a logged-off session is never bridged.
func (r *sessionRegistry) clearLogin(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.byID[id]; e != nil {
		e.view.Username = ""
		e.view.LoggedInAt = time.Time{}
		e.view.Service = ""
	}
}

// setService records the service NAME an entry is actively bridged to.
func (r *sessionRegistry) setService(id uint64, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.byID[id]; e != nil {
		e.view.Service = name
	}
}

// clearService records that an entry's bridge has ended (back to the menu).
func (r *sessionRegistry) clearService(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.byID[id]; e != nil {
		e.view.Service = ""
	}
}

// SessionRegistry is the read/act seam the admin flow depends on (so admin
// tests use a fake and the screen layer never sees concurrency internals).
// *sessionRegistry satisfies it.
type SessionRegistry interface {
	Snapshot() []SessionView
	Disconnect(id uint64) (SessionView, bool)
}

var _ SessionRegistry = (*sessionRegistry)(nil)

// Disconnect hard-closes the session's connection and returns its last-known
// view for the audit record. ok=false means the id was already gone (it raced a
// natural disconnect). The entry is NOT removed here — the session goroutine's
// own teardown defer deregisters once its blocked Read unblocks. close() is
// invoked outside the lock (it is a syscall) and is idempotent.
func (r *sessionRegistry) Disconnect(id uint64) (SessionView, bool) {
	r.mu.Lock()
	e := r.byID[id]
	if e == nil {
		r.mu.Unlock()
		return SessionView{}, false
	}
	view, closeFn := e.view, e.close
	r.mu.Unlock()
	if closeFn != nil {
		closeFn()
	}
	return view, true
}
