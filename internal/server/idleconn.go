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
	"net"
	"sync"
	"time"
)

// idleConn wraps a net.Conn and arms a deadline before every Read and Write so
// a stalled peer cannot pin the connection (slowloris hardening, GH #1/#18).
//
// Two bounds compose: a sliding idle window (idle) re-armed on every byte, and
// an optional absolute ceiling (hard) that does NOT slide — used pre-auth so a
// trickle cannot hold a slot forever. The earlier of the two fires. idle<=0
// with no ceiling disables the deadline (trusted/exempt regimes).
//
// An explicit non-zero deadline set through the wrapper suspends auto-arming
// until a zero deadline clears it (the bridge teardown interrupt relies on
// this). mu serializes deadline decisions.
type idleConn struct {
	net.Conn

	mu     sync.Mutex
	idle   time.Duration
	hard   time.Time // absolute ceiling; zero = none
	manual bool      // explicit deadline in force; auto-arm suspended
}

func newIdleConn(c net.Conn, idle time.Duration) *idleConn {
	return &idleConn{Conn: c, idle: idle}
}

// arm sets the deadline to the earlier of (now+idle) and the absolute ceiling,
// unless an explicit deadline is in force. A zero result clears the deadline.
func (c *idleConn) arm() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.manual {
		return
	}
	var d time.Time
	if c.idle > 0 {
		d = time.Now().Add(c.idle)
	}
	if !c.hard.IsZero() && (d.IsZero() || c.hard.Before(d)) {
		d = c.hard
	}
	c.Conn.SetDeadline(d)
}

// setPreAuth installs the pre-auth regime: a sliding idle window plus an
// absolute ceiling now+max by which authentication must complete. The ceiling
// does not slide with activity; re-call to re-arm it (e.g. at logoff).
func (c *idleConn) setPreAuth(idle, max time.Duration) {
	c.mu.Lock()
	c.idle = idle
	c.hard = time.Now().Add(max)
	c.mu.Unlock()
}

// setWindow installs a plain sliding idle window with no ceiling (post-auth and
// bridge regimes). idle<=0 disables the deadline entirely (trusted pre-auth
// exemption, or bridge_idle=exempt).
func (c *idleConn) setWindow(idle time.Duration) {
	c.mu.Lock()
	c.idle = idle
	c.hard = time.Time{}
	c.mu.Unlock()
}

func (c *idleConn) Read(p []byte) (int, error) {
	c.arm()
	return c.Conn.Read(p)
}

func (c *idleConn) Write(p []byte) (int, error) {
	c.arm()
	return c.Conn.Write(p)
}

// SetDeadline passes through, suspending auto-arm while a non-zero explicit
// deadline is in force; a zero deadline resumes idle enforcement.
func (c *idleConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.manual = !t.IsZero()
	return c.Conn.SetDeadline(t)
}

func (c *idleConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.manual = !t.IsZero()
	return c.Conn.SetReadDeadline(t)
}

func (c *idleConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.manual = !t.IsZero()
	return c.Conn.SetWriteDeadline(t)
}
