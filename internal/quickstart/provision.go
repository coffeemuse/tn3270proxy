package quickstart

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	"github.com/CoffeeMuse/tn3270proxy/internal/seed"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/CoffeeMuse/tn3270proxy/internal/sysconfig"
)

// ErrAlreadyProvisioned signals that the data dir already has a config file and
// must not be touched (the normal post-first-boot path).
var ErrAlreadyProvisioned = errors.New("quickstart: data dir already provisioned")

// Provision classifies dir and, when fresh, generates every quick-start
// artifact, writing the config file last as the commit point. Returns
// ErrAlreadyProvisioned when a config file is already present, or a partial-dir
// error when proxy.db exists without a config.
func Provision(ctx context.Context, dir string) (*Result, error) {
	l := NewLayout(dir)
	if _, err := os.Stat(l.Config); err == nil {
		return nil, ErrAlreadyProvisioned
	}
	if _, err := os.Stat(l.DB); err == nil {
		return nil, fmt.Errorf("data dir %s is partially provisioned or not empty (found proxy.db but no tn3270proxy.json); remove its contents and retry", dir)
	}

	if err := os.MkdirAll(l.CertDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	st, err := store.Open(l.DB)
	if err != nil {
		return nil, err
	}
	defer st.Close()

	// Generate credentials.
	adminPw, err := GenPassword()
	if err != nil {
		return nil, err
	}
	res := &Result{
		DataDir:     dir,
		Admin:       Cred{Username: AdminUser, Password: adminPw, Groups: []string{store.AdminGroup, DemoGroup}},
		PlainAddr:   PlainAddr,
		TLSAddr:     TLSAddr,
		DemoService: DemoServiceName,
	}
	for _, name := range SampleUsers {
		pw, err := GenPassword()
		if err != nil {
			return nil, err
		}
		res.Samples = append(res.Samples, Cred{Username: name, Password: pw, Groups: []string{DemoGroup}})
	}

	// Seed users, groups, and the demo service in one shot.
	if err := seed.Apply(ctx, st, buildSeedData(*res)); err != nil {
		return nil, fmt.Errorf("seed: %w", err)
	}

	// MFA master key: 32 random bytes, base64, mode 0600.
	keyRaw := make([]byte, 32)
	if _, err := rand.Read(keyRaw); err != nil {
		return nil, fmt.Errorf("mfa key: %w", err)
	}
	if err := os.WriteFile(l.MFAKey, []byte(base64.StdEncoding.EncodeToString(keyRaw)), 0o600); err != nil {
		return nil, fmt.Errorf("write mfa key: %w", err)
	}

	// Self-signed cert.
	certPEM, keyPEM, err := GenerateSelfSignedCert(CertHosts, CertValidDays)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(l.Cert, certPEM, 0o644); err != nil {
		return nil, fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(l.Key, keyPEM, 0o600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}

	// MOTD file + sysconfig pointer.
	if err := os.WriteFile(l.MOTD, []byte(DefaultMOTD()), 0o644); err != nil {
		return nil, fmt.Errorf("write motd: %w", err)
	}
	if err := st.SetConfig(ctx, sysconfig.KeyMOTDFile, l.MOTD); err != nil {
		return nil, fmt.Errorf("set motd config: %w", err)
	}

	// Human-readable credential record. World-readable (0644) on purpose: it is
	// written into a root-owned bind mount and the host user must be able to open
	// it. Secrets (mfa.key, key.pem) stay 0600. The file tells the user to delete
	// it after recording the credentials.
	if err := os.WriteFile(l.SetupFile, []byte(renderSetupFile(*res)), 0o644); err != nil {
		return nil, fmt.Errorf("write setup file: %w", err)
	}

	// Config LAST — its presence marks the dir as fully provisioned.
	cfgBytes, err := renderConfigJSON(l)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(l.Config, cfgBytes, 0o644); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}
	return res, nil
}
