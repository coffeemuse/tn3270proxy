package server

import (
	"crypto/tls"
	"testing"
)

func TestBackendTLSConfig(t *testing.T) {
	if cfg := backendTLSConfig("h:23", BackendTLS{Enabled: false, Verify: true}); cfg != nil {
		t.Errorf("disabled → want nil config, got %+v", cfg)
	}

	on := backendTLSConfig("cics.corp:992", BackendTLS{Enabled: true, Verify: true})
	if on == nil {
		t.Fatal("enabled → want non-nil config")
	}
	if on.InsecureSkipVerify {
		t.Errorf("verify on → InsecureSkipVerify must be false")
	}
	if on.ServerName != "cics.corp" {
		t.Errorf("ServerName = %q, want host %q", on.ServerName, "cics.corp")
	}
	if on.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want TLS 1.2 (%d)", on.MinVersion, tls.VersionTLS12)
	}

	off := backendTLSConfig("10.0.0.5:992", BackendTLS{Enabled: true, Verify: false})
	if off == nil || !off.InsecureSkipVerify {
		t.Errorf("verify off → InsecureSkipVerify must be true, got %+v", off)
	}
	if off.ServerName != "10.0.0.5" {
		t.Errorf("ServerName = %q, want host %q", off.ServerName, "10.0.0.5")
	}
}
