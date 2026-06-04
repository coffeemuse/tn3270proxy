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

package bridge

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeBackend accepts one connection, performs minimal Telnet negotiation
// (DO TERMINAL-TYPE / SB SEND), then echoes any 3270 data it receives back to
// the client. It records the terminal type the client reported.
type fakeBackend struct {
	ln       net.Listener
	termType chan string
}

func startFakeBackend(t *testing.T) *fakeBackend {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fb := &fakeBackend{ln: ln, termType: make(chan string, 1)}
	go fb.serve()
	t.Cleanup(func() { ln.Close() })
	return fb
}

func (fb *fakeBackend) addr() string { return fb.ln.Addr().String() }

func (fb *fakeBackend) serve() {
	conn, err := fb.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	// Ask for terminal type.
	conn.Write([]byte{cIAC, cDO, optTERMTYPE})
	conn.Write([]byte{cIAC, cSB, optTERMTYPE, ttSEND, cIAC, cSE})

	buf := make([]byte, 4096)
	gotTT := false
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			data := buf[:n]
			// Crudely capture the terminal type from the IS subnegotiation.
			if !gotTT {
				if i := indexSub(data); i >= 0 {
					fb.termType <- string(data[i:])
					gotTT = true
				}
			}
			// Echo back any application data (used by the relay test).
			conn.Write(data)
		}
		if err != nil {
			return
		}
	}
}

// indexSub returns the start index of the terminal-type string inside an
// IAC SB TERMINAL-TYPE IS <name> IAC SE sequence, or -1.
func indexSub(b []byte) int {
	for i := 0; i+3 < len(b); i++ {
		if b[i] == cIAC && b[i+1] == cSB && b[i+2] == optTERMTYPE && b[i+3] == ttIS {
			return i + 4
		}
	}
	return -1
}

func TestBridgeNegotiatesAndRelays(t *testing.T) {
	fb := startFakeBackend(t)

	// client side of an in-memory pipe stands in for the end-user emulator.
	clientConn, proxySide := net.Pipe()
	defer clientConn.Close()

	done := make(chan Cause, 1)
	go func() {
		c, err := Bridge(proxySide, fb.addr(), "IBM-3278-2-E", aidPA3, nil)
		if err != nil {
			t.Errorf("Bridge error: %v", err)
		}
		done <- c
	}()

	// The fake backend should learn our terminal type via negotiation.
	select {
	case tt := <-fb.termType:
		if !strings.HasPrefix(tt, "IBM-3278-2-E") {
			t.Errorf("backend got termtype %q", tt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backend never received terminal type")
	}

	// Send a 3270 data record from the client; expect it echoed back.
	rec := []byte{0xF5, 0xC3, 0x11, 0x40, 0x40, cIAC, cEOR}
	if _, err := clientConn.Write(rec); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(rec))
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := readFull(clientConn, got); err != nil {
		t.Fatalf("reading echo: %v", err)
	}
	for i := range rec {
		if got[i] != rec[i] {
			t.Errorf("echo[%d] = %#x, want %#x", i, got[i], rec[i])
		}
	}
}

func TestBridgeEscapeOnPA3(t *testing.T) {
	fb := startFakeBackend(t)
	clientConn, proxySide := net.Pipe()
	defer clientConn.Close()

	done := make(chan Cause, 1)
	go func() {
		c, _ := Bridge(proxySide, fb.addr(), "IBM-3278-2-E", aidPA3, nil)
		done <- c
	}()

	// Client presses PA3 → a record beginning with 0x6B.
	clientConn.Write([]byte{aidPA3, cIAC, cEOR})

	select {
	case c := <-done:
		if c != CauseUserEscaped {
			t.Errorf("cause = %v, want CauseUserEscaped", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not return on PA3")
	}
}

func TestBridgeDialError(t *testing.T) {
	clientConn, proxySide := net.Pipe()
	defer clientConn.Close()
	c, err := Bridge(proxySide, "127.0.0.1:1", "IBM-3278-2-E", aidPA3, nil)
	if err == nil {
		t.Errorf("expected dial error")
	}
	if c != CauseError {
		t.Errorf("cause = %v, want CauseError", c)
	}
}

// selfSignedCert returns a TLS server certificate valid for 127.0.0.1 and a
// pool trusting it. Used to exercise the bridge's TLS dial path.
func selfSignedCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Unix(0, 0),
		NotAfter:     time.Unix(1<<31-1, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// startFakeTLSBackend is the TLS twin of startFakeBackend: same Telnet/echo
// serve loop, behind a tls.NewListener.
func startFakeTLSBackend(t *testing.T, cert tls.Certificate) *fakeBackend {
	t.Helper()
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := tls.NewListener(raw, &tls.Config{Certificates: []tls.Certificate{cert}})
	fb := &fakeBackend{ln: ln, termType: make(chan string, 1)}
	go fb.serve()
	t.Cleanup(func() { ln.Close() })
	return fb
}

func TestBridgeTLSBackendVerified(t *testing.T) {
	cert, pool := selfSignedCert(t)
	fb := startFakeTLSBackend(t, cert)

	clientConn, proxySide := net.Pipe()
	defer clientConn.Close()

	host, _, _ := net.SplitHostPort(fb.addr())
	tlsCfg := &tls.Config{RootCAs: pool, ServerName: host}

	done := make(chan Cause, 1)
	go func() {
		c, err := Bridge(proxySide, fb.addr(), "IBM-3278-2-E", aidPA3, tlsCfg)
		if err != nil {
			t.Errorf("Bridge error: %v", err)
		}
		done <- c
	}()

	select {
	case tt := <-fb.termType:
		if !strings.HasPrefix(tt, "IBM-3278-2-E") {
			t.Errorf("backend got termtype %q", tt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backend never received terminal type over TLS")
	}

	rec := []byte{0xF5, 0xC3, 0x11, 0x40, 0x40, cIAC, cEOR}
	if _, err := clientConn.Write(rec); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(rec))
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := readFull(clientConn, got); err != nil {
		t.Fatalf("reading echo over TLS: %v", err)
	}
	for i := range rec {
		if got[i] != rec[i] {
			t.Errorf("echo[%d] = %#x, want %#x", i, got[i], rec[i])
		}
	}
}

func TestBridgeTLSBackendUntrustedFails(t *testing.T) {
	cert, _ := selfSignedCert(t)
	fb := startFakeTLSBackend(t, cert)

	clientConn, proxySide := net.Pipe()
	defer clientConn.Close()

	host, _, _ := net.SplitHostPort(fb.addr())
	// No RootCAs → system roots → self-signed cert is untrusted → handshake fails.
	tlsCfg := &tls.Config{ServerName: host}

	c, err := Bridge(proxySide, fb.addr(), "IBM-3278-2-E", aidPA3, tlsCfg)
	if err == nil {
		t.Errorf("expected TLS verification error")
	}
	if c != CauseError {
		t.Errorf("cause = %v, want CauseError", c)
	}
}

// readFull reads len(buf) bytes or returns an error.
func readFull(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
