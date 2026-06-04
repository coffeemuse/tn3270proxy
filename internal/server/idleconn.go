package server

import (
	"net"
	"sync"
	"time"
)

// idleConn wraps a net.Conn and arms an idle deadline (SetDeadline(now+idle))
// before every Read and Write, so a stalled client cannot pin the connection
// forever (slowloris hardening, GH issue #1).
//
// An explicit non-zero deadline set *through* the wrapper suspends auto-arming
// until a zero deadline clears it again. The bridge relies on this: it
// interrupts a blocked relay by setting a past deadline (bridge.go), and a
// blind re-arm from the other relay goroutine would overwrite that interrupt
// and stall teardown. mu serializes deadline decisions so the
// suspend-vs-re-arm interleaving cannot race.
type idleConn struct {
	net.Conn

	mu     sync.Mutex
	idle   time.Duration
	manual bool // explicit deadline in force; auto-arm suspended
}

func newIdleConn(c net.Conn, idle time.Duration) *idleConn {
	return &idleConn{Conn: c, idle: idle}
}

// SetIdle changes the idle window for subsequent reads/writes (e.g. the
// pre-auth → post-auth switch).
func (c *idleConn) SetIdle(d time.Duration) {
	c.mu.Lock()
	c.idle = d
	c.mu.Unlock()
}

// arm sets the idle deadline unless an explicit deadline is in force.
func (c *idleConn) arm() {
	c.mu.Lock()
	if !c.manual {
		c.Conn.SetDeadline(time.Now().Add(c.idle))
	}
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
