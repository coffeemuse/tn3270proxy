package quickstart

import (
	"path/filepath"
	"testing"
)

func TestNewLayout(t *testing.T) {
	l := NewLayout("/data")
	cases := map[string]string{
		"config":    l.Config,
		"db":        l.DB,
		"mfakey":    l.MFAKey,
		"cert":      l.Cert,
		"key":       l.Key,
		"motd":      l.MOTD,
		"branding":  l.Branding,
		"setupfile": l.SetupFile,
	}
	want := map[string]string{
		"config":    "/data/tn3270proxy.json",
		"db":        "/data/proxy.db",
		"mfakey":    "/data/mfa.key",
		"cert":      "/data/tls/cert.pem",
		"key":       "/data/tls/key.pem",
		"motd":      "/data/motd.txt",
		"branding":  "/data/branding.txt",
		"setupfile": "/data/SETUP-DEFAULTS.TXT",
	}
	for k, got := range cases {
		if got != filepath.FromSlash(want[k]) {
			t.Errorf("%s = %q, want %q", k, got, want[k])
		}
	}
}
