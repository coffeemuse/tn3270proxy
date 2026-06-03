package bridge

import (
	"net"
	"time"
)

// Cause explains why a bridged session ended.
type Cause int

const (
	CauseError         Cause = iota // dial or I/O error
	CauseBackendClosed              // backend host closed the connection
	CauseClientClosed               // end user disconnected
	CauseUserEscaped                // user pressed the escape AID (PA3)
)

// dialTimeout bounds how long we wait to connect to a backend.
const dialTimeout = 10 * time.Second

// pastDeadline is any time in the past; setting it as a deadline forces a
// blocked Read/Write to return immediately. We use a fixed constant so the
// code has no dependency on the wall clock for teardown.
var pastDeadline = time.Unix(1, 0)

// Bridge dials the backend at addr, negotiates the client leg of Telnet
// (offering termType), and relays the 3270 datastream between client and
// backend until one side closes or the user presses escapeAID. The client
// connection is NOT closed (the caller reuses it for the menu); its deadlines
// are reset before returning.
func Bridge(client net.Conn, addr, termType string, escapeAID byte) (Cause, error) {
	backend, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return CauseError, err
	}
	defer backend.Close()

	results := make(chan Cause, 2)

	// Concurrency note: both goroutines may write to the same connection — the
	// backend→client goroutine forwards 3270 data to client while the
	// client→backend goroutine writes negotiation replies to client (and
	// symmetrically for backend). net.Conn.Write is goroutine-safe, so this is
	// not a data race. In the MVP, Telnet negotiation is front-loaded and data
	// flows afterward, so reply and data writes do not meaningfully overlap in
	// practice. If mid-session renegotiation is added later, interleaving of a
	// negotiation reply with a forwarded data chunk on the same connection
	// becomes a real hazard to revisit.

	// backend → client: proxy answers Telnet as a client toward the backend.
	go func() {
		p := newProcessor(roleClient, termType, 0)
		results <- relay(backend, client, p, CauseBackendClosed)
	}()
	// client → backend: proxy answers Telnet as a server; watches for escape.
	go func() {
		p := newProcessor(roleServer, "", escapeAID)
		results <- relay(client, backend, p, CauseClientClosed)
	}()

	first := <-results
	// Interrupt the still-running direction so its Read returns.
	backend.SetDeadline(pastDeadline)
	client.SetDeadline(pastDeadline)
	<-results

	// Reset client deadlines so the connection is reusable for the menu.
	client.SetDeadline(time.Time{})
	return first, nil
}

// relay reads from src, processes Telnet, writes negotiation replies back to
// src and forwardable data to dst. It returns CauseUserEscaped if the processor
// detects the escape AID, closeCause when src reaches EOF/closes, or
// CauseError on a write failure.
func relay(src, dst net.Conn, p *telnetProcessor, closeCause Cause) Cause {
	buf := make([]byte, 4096)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			fwd, reply, escaped := p.process(buf[:n])
			if len(reply) > 0 {
				// net.Conn.Write returns a non-nil error on a short write, so
				// discarding the byte count is correct — any partial write
				// surfaces here as CauseError.
				if _, werr := src.Write(reply); werr != nil {
					return CauseError
				}
			}
			if len(fwd) > 0 {
				// Same short-write guarantee as above.
				if _, werr := dst.Write(fwd); werr != nil {
					return CauseError
				}
			}
			if escaped {
				return CauseUserEscaped
			}
		}
		if err != nil {
			return closeCause
		}
	}
}
