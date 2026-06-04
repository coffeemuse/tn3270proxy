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
	"log"
	"net"
	"sync"
)

// connLimiter bounds concurrent connections globally and per client IP
// (slowloris/DoS hardening, GH issue #1). The global slot is acquired BEFORE
// Accept, so over-cap connections wait in the kernel backlog at no cost to
// the process and are served when load subsides. The per-IP check needs the
// remote address, so it runs after Accept; over-cap conns are closed.
// A single limiter is shared by every listener (ServeAll).
type connLimiter struct {
	sem      chan struct{} // global cap
	maxPerIP int           // 0 disables the per-IP check

	mu    sync.Mutex
	perIP map[string]int
}

func newConnLimiter(maxConns, maxPerIP int) *connLimiter {
	return &connLimiter{
		sem:      make(chan struct{}, maxConns),
		maxPerIP: maxPerIP,
		perIP:    make(map[string]int),
	}
}

// acquire claims a global slot, blocking while the cap is full. Logs once per
// full-cap episode so an attack (or undersized cap) is visible.
func (l *connLimiter) acquire() {
	if l == nil {
		return
	}
	select {
	case l.sem <- struct{}{}:
		return
	default:
		log.Printf("connection cap (%d) reached; deferring accepts", cap(l.sem))
		l.sem <- struct{}{}
	}
}

// releaseGlobal frees a global slot.
func (l *connLimiter) releaseGlobal() {
	if l == nil {
		return
	}
	<-l.sem
}

// ipKey reduces a remote address to its host IP; "" if unparseable.
func ipKey(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return ""
	}
	return host
}

// admitIP records one connection against addr's IP, reporting false when the
// per-IP cap is already reached (the caller must then close the conn and must
// NOT call releaseIP for it).
func (l *connLimiter) admitIP(addr net.Addr) bool {
	if l == nil || l.maxPerIP <= 0 {
		return true
	}
	key := ipKey(addr)
	if key == "" {
		return true // never lock out a conn we can't attribute
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.perIP[key] >= l.maxPerIP {
		return false
	}
	l.perIP[key]++
	return true
}

// releaseIP undoes admitIP for addr's IP.
func (l *connLimiter) releaseIP(addr net.Addr) {
	if l == nil || l.maxPerIP <= 0 {
		return
	}
	key := ipKey(addr)
	if key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if n := l.perIP[key]; n <= 1 {
		delete(l.perIP, key) // don't let the map grow with dead IPs
	} else {
		l.perIP[key] = n - 1
	}
}
