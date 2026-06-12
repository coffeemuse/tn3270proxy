package quickstart

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/coffeemuse/tn3270proxy/internal/auth"
	"github.com/coffeemuse/tn3270proxy/internal/config"
	"github.com/coffeemuse/tn3270proxy/internal/store"
	"github.com/coffeemuse/tn3270proxy/internal/sysconfig"
)

func TestProvisionFresh(t *testing.T) {
	dir := t.TempDir()
	res, err := Provision(context.Background(), dir)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	l := NewLayout(dir)
	for _, p := range []string{l.Config, l.DB, l.MFAKey, l.Cert, l.Key, l.MOTD, l.Branding, l.SetupFile} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected artifact %s: %v", p, err)
		}
	}
	// Generated config loads through the real loader.
	cfg, err := config.Load([]string{"-config", l.Config})
	if err != nil {
		t.Fatalf("config.Load on generated config: %v", err)
	}
	if !cfg.Plain.Enabled || !cfg.TLS.Enabled {
		t.Errorf("both listeners should be enabled: %+v", cfg)
	}
	if len(cfg.MFA.Key) != 32 {
		t.Errorf("mfa key should decode to 32 bytes, got %d", len(cfg.MFA.Key))
	}
	// mfa.key file is base64 of 32 bytes.
	raw, _ := os.ReadFile(l.MFAKey)
	if dec, err := base64.StdEncoding.DecodeString(string(raw)); err != nil || len(dec) != 32 {
		t.Errorf("mfa.key not base64 of 32 bytes: err=%v len=%d", err, len(dec))
	}
	// Admin password verifies against the stored bcrypt hash.
	st, err := store.Open(l.DB)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer st.Close()
	if _, err := st.GetUserByUsername(context.Background(), res.Admin.Username); err != nil {
		t.Fatalf("get admin: %v", err)
	}
	if _, err := auth.Authenticate(context.Background(), st, res.Admin.Username, res.Admin.Password); err != nil {
		t.Errorf("admin password should authenticate: %v", err)
	}
	// MOTD_FILE sysconfig points at the motd file.
	mv, err := st.GetConfig(context.Background(), sysconfig.KeyMOTDFile)
	if err != nil {
		t.Fatalf("get motd config: %v", err)
	}
	if mv != l.MOTD {
		t.Errorf("MOTD_FILE = %q, want %q", mv, l.MOTD)
	}
	// BRANDING_FILE sysconfig points at the branding file.
	bv, err := st.GetConfig(context.Background(), sysconfig.KeyBrandingFile)
	if err != nil {
		t.Fatalf("get branding config: %v", err)
	}
	if bv != l.Branding {
		t.Errorf("BRANDING_FILE = %q, want %q", bv, l.Branding)
	}
}

func TestProvisionExistingIsNoOp(t *testing.T) {
	dir := t.TempDir()
	if _, err := Provision(context.Background(), dir); err != nil {
		t.Fatalf("first Provision: %v", err)
	}
	l := NewLayout(dir)
	if err := os.WriteFile(l.SetupFile, []byte("SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Provision(context.Background(), dir)
	if !errors.Is(err, ErrAlreadyProvisioned) {
		t.Fatalf("want ErrAlreadyProvisioned, got %v", err)
	}
	b, _ := os.ReadFile(l.SetupFile)
	if string(b) != "SENTINEL" {
		t.Errorf("existing dir was mutated; SETUP file changed")
	}
}

func TestProvisionPartialDirErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "proxy.db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Provision(context.Background(), dir)
	if err == nil || errors.Is(err, ErrAlreadyProvisioned) {
		t.Fatalf("want a partial-dir error, got %v", err)
	}
}

func TestProvisionSeedsDocuments(t *testing.T) {
	dir := t.TempDir()
	if _, err := Provision(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(NewLayout(dir).DB)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for name, want := range map[string]string{
		store.DocMOTD:     DefaultMOTD(),
		store.DocBranding: DefaultBranding(),
	} {
		d, err := st.GetDocument(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		if d.Content != want || d.UpdatedBy != "quickstart" {
			t.Errorf("%s: content match=%v updated_by=%q", name, d.Content == want, d.UpdatedBy)
		}
	}
	// HELP-MENU is seeded by reconcileDefaults (stock text), not by quickstart.
	h, err := st.GetDocument(context.Background(), store.DocHelpMenu)
	if err != nil {
		t.Fatal(err)
	}
	if h.Content == "" {
		t.Error("HELP-MENU should hold the stock help text after provisioning")
	}
}
