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

package listen

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/config"
)

// genSelfSigned writes a self-signed cert/key valid for 127.0.0.1 into a temp
// dir and returns their paths.
func genSelfSigned(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Unix(0, 0),
		NotAfter:     time.Unix(1<<31-1, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	certOut, err := os.Create(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	certOut.Close()

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyOut, err := os.Create(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		t.Fatal(err)
	}
	keyOut.Close()
	return certPath, keyPath
}

func TestBuildPlain(t *testing.T) {
	cfg := config.Config{Plain: config.Listener{Enabled: true, Addr: "127.0.0.1:0"}}
	lns, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if len(lns) != 1 {
		t.Fatalf("len(lns) = %d, want 1", len(lns))
	}
	defer lns[0].Close()

	conn, err := net.Dial("tcp", lns[0].Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()
}

func TestBuildTLSRoundTrips(t *testing.T) {
	cert, key := genSelfSigned(t)
	cfg := config.Config{TLS: config.TLSListener{
		Enabled: true, Addr: "127.0.0.1:0", Cert: cert, Key: key,
	}}
	lns, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if len(lns) != 1 {
		t.Fatalf("len(lns) = %d, want 1", len(lns))
	}
	defer lns[0].Close()

	got := make(chan string, 1)
	go func() {
		c, err := lns[0].Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 4)
		n, _ := c.Read(buf)
		got <- string(buf[:n])
	}()

	client, err := tls.Dial("tcp", lns[0].Addr().String(), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("tls.Dial: %v", err)
	}
	defer client.Close()
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case s := <-got:
		if s != "ping" {
			t.Errorf("server read %q, want \"ping\"", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not read over TLS")
	}
}

func TestBuildTLSBadCertErrors(t *testing.T) {
	cfg := config.Config{TLS: config.TLSListener{
		Enabled: true, Addr: "127.0.0.1:0", Cert: "/no/cert.pem", Key: "/no/key.pem",
	}}
	if _, err := Build(cfg); err == nil {
		t.Fatal("want error for unreadable cert, got nil")
	}
}
