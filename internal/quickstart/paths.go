// Package quickstart provisions a fresh data directory for the opinionated
// Docker quick-start: a generated admin, sample users, a demo service, an MFA
// key, a self-signed cert, a MOTD, and a human-readable credentials file.
package quickstart

import "path/filepath"

const (
	PlainAddr = ":2323"
	TLSAddr   = ":2324"

	AdminUser       = "ADMIN"
	DemoGroup       = "DEMO"
	DemoServiceName = "DEMO"
	DemoBackendHost = "dummy3270"
	DemoBackendPort = 3300

	CertValidDays = 825
)

// SampleUsers are the non-admin demo accounts seeded on first run.
var SampleUsers = []string{"OPERATOR", "GUEST"}

// CertHosts are the SAN entries for the generated self-signed cert.
var CertHosts = []string{"localhost", "127.0.0.1", "::1"}

// Layout maps a data directory to every artifact quickstart manages.
type Layout struct {
	Dir       string
	DB        string
	Config    string
	MFAKey    string
	CertDir   string
	Cert      string
	Key       string
	MOTD      string
	SetupFile string
}

// NewLayout derives all artifact paths under dir.
func NewLayout(dir string) Layout {
	return Layout{
		Dir:       dir,
		DB:        filepath.Join(dir, "proxy.db"),
		Config:    filepath.Join(dir, "tn3270proxy.json"),
		MFAKey:    filepath.Join(dir, "mfa.key"),
		CertDir:   filepath.Join(dir, "tls"),
		Cert:      filepath.Join(dir, "tls", "cert.pem"),
		Key:       filepath.Join(dir, "tls", "key.pem"),
		MOTD:      filepath.Join(dir, "motd.txt"),
		SetupFile: filepath.Join(dir, "SETUP-DEFAULTS.TXT"),
	}
}
