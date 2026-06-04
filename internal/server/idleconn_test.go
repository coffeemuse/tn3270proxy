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
	"net"
	"testing"
	"time"
)

// isTimeout reports whether err is a net timeout error.
func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func TestIdleConnReadTimesOutWhenIdle(t *testing.T) {
	c, _ := net.Pipe()
	defer c.Close()
	ic := newIdleConn(c, 50*time.Millisecond)

	start := time.Now()
	_, err := ic.Read(make([]byte, 1))
	if !isTimeout(err) {
		t.Fatalf("Read error = %v, want timeout", err)
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("timed out after %v, want >= ~50ms", elapsed)
	}
}

func TestIdleConnReadRearmsPerCall(t *testing.T) {
	c, peer := net.Pipe()
	defer c.Close()
	defer peer.Close()
	ic := newIdleConn(c, 60*time.Millisecond)

	// Feed one byte after 40ms — inside the idle window, so the first read
	// succeeds; the second read must get a fresh 60ms window, not the remnant.
	go func() {
		time.Sleep(40 * time.Millisecond)
		peer.Write([]byte{0xAA})
	}()
	if _, err := ic.Read(make([]byte, 1)); err != nil {
		t.Fatalf("first Read error = %v, want data", err)
	}
	start := time.Now()
	_, err := ic.Read(make([]byte, 1))
	if !isTimeout(err) {
		t.Fatalf("second Read error = %v, want timeout", err)
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("second read timed out after %v: idle window was not re-armed", elapsed)
	}
}

func TestIdleConnWriteTimesOutWhenPeerStalls(t *testing.T) {
	c, peer := net.Pipe()
	defer c.Close()
	defer peer.Close() // peer never reads: a zero-window client
	ic := newIdleConn(c, 50*time.Millisecond)

	_, err := ic.Write([]byte("screen data"))
	if !isTimeout(err) {
		t.Fatalf("Write error = %v, want timeout", err)
	}
}

func TestIdleConnExplicitDeadlineSuspendsAutoArm(t *testing.T) {
	c, _ := net.Pipe()
	defer c.Close()
	ic := newIdleConn(c, 10*time.Second)

	// The bridge's interrupt: a past deadline set through the wrapper must NOT
	// be overwritten by the next Read's auto-arm (which would block ~10s).
	if err := ic.SetDeadline(time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}
	start := time.Now()
	_, err := ic.Read(make([]byte, 1))
	if !isTimeout(err) {
		t.Fatalf("Read error = %v, want timeout", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Read blocked %v: auto-arm overwrote the explicit past deadline", elapsed)
	}
}

func TestIdleConnZeroDeadlineResumesAutoArm(t *testing.T) {
	c, _ := net.Pipe()
	defer c.Close()
	ic := newIdleConn(c, 50*time.Millisecond)

	ic.SetDeadline(time.Now().Add(-time.Hour)) // suspend (bridge interrupt)
	ic.SetDeadline(time.Time{})                // clear (bridge reset for menu reuse)

	start := time.Now()
	_, err := ic.Read(make([]byte, 1))
	if !isTimeout(err) {
		t.Fatalf("Read error = %v, want timeout", err)
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("timed out after %v: idle timeout did not resume after zero deadline", elapsed)
	}
}

func TestIdleConnSetIdleSwitchesWindow(t *testing.T) {
	c, _ := net.Pipe()
	defer c.Close()
	ic := newIdleConn(c, 10*time.Second) // pre-auth value

	ic.SetIdle(50 * time.Millisecond) // post-auth switch (inverted for test speed)

	start := time.Now()
	_, err := ic.Read(make([]byte, 1))
	if !isTimeout(err) {
		t.Fatalf("Read error = %v, want timeout", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Read blocked %v: SetIdle did not take effect", elapsed)
	}
}
