package quickstart

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"testing"
	"time"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	certPEM, keyPEM, err := GenerateSelfSignedCert([]string{"localhost", "127.0.0.1", "::1"}, 825)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert: %v", err)
	}
	// Loadable as a TLS keypair (what listen.Build does).
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}
	// Parse the leaf and assert SANs + validity window.
	pair, _ := tls.X509KeyPair(certPEM, keyPEM)
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	if err := leaf.VerifyHostname("localhost"); err != nil {
		t.Errorf("VerifyHostname(localhost): %v", err)
	}
	for _, want := range []string{"127.0.0.1", "::1"} {
		found := false
		for _, ip := range leaf.IPAddresses {
			if ip.Equal(net.ParseIP(want)) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s in IP SANs, got %v", want, leaf.IPAddresses)
		}
	}
	wantNotAfter := leaf.NotBefore.Add(825 * 24 * time.Hour)
	if leaf.NotAfter.Sub(wantNotAfter) > time.Hour || wantNotAfter.Sub(leaf.NotAfter) > time.Hour {
		t.Errorf("NotAfter = %v, want ~%v", leaf.NotAfter, wantNotAfter)
	}
}
