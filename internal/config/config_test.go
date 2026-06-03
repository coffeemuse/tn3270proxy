package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	c, err := Load(nil)
	if err != nil {
		t.Fatalf("Load(nil) error: %v", err)
	}
	if !c.Plain.Enabled || c.Plain.Addr != ":2323" {
		t.Errorf("Plain = %+v, want {Enabled:true Addr:\":2323\"}", c.Plain)
	}
	if c.TLS.Enabled {
		t.Errorf("TLS.Enabled = true, want false by default")
	}
	if c.DBPath != "tn3270proxy.db" {
		t.Errorf("DBPath = %q, want \"tn3270proxy.db\"", c.DBPath)
	}
}

func TestLoadFlagOverrides(t *testing.T) {
	c, err := Load([]string{"-listen", "127.0.0.1:9999", "-db", "/tmp/x.db"})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.Plain.Addr != "127.0.0.1:9999" {
		t.Errorf("Plain.Addr = %q", c.Plain.Addr)
	}
	if c.DBPath != "/tmp/x.db" {
		t.Errorf("DBPath = %q", c.DBPath)
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tn3270proxy.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadConfigFile(t *testing.T) {
	p := writeConfig(t, `{
		"db": "/data/p.db",
		"listeners": {
			"plain": { "enabled": false, "addr": ":111" },
			"tls": { "enabled": true, "addr": ":3270", "cert": "c.pem", "key": "k.pem" }
		}
	}`)
	c, err := Load([]string{"-config", p})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.DBPath != "/data/p.db" {
		t.Errorf("DBPath = %q", c.DBPath)
	}
	if c.Plain.Enabled {
		t.Errorf("Plain.Enabled = true, want false")
	}
	if !c.TLS.Enabled || c.TLS.Addr != ":3270" || c.TLS.Cert != "c.pem" || c.TLS.Key != "k.pem" {
		t.Errorf("TLS = %+v", c.TLS)
	}
}

func TestFlagOverridesFile(t *testing.T) {
	p := writeConfig(t, `{ "listeners": { "plain": { "enabled": true, "addr": ":111" } } }`)
	c, err := Load([]string{"-config", p, "-listen", ":222", "-db", "/flag.db"})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.Plain.Addr != ":222" {
		t.Errorf("Plain.Addr = %q, want flag value \":222\"", c.Plain.Addr)
	}
	if c.DBPath != "/flag.db" {
		t.Errorf("DBPath = %q, want \"/flag.db\"", c.DBPath)
	}
}

func TestExplicitConfigMissingIsError(t *testing.T) {
	_, err := Load([]string{"-config", "/no/such/file.json"})
	if err == nil {
		t.Fatal("want error for explicit missing config, got nil")
	}
}

func TestDefaultConfigFilePickup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tn3270proxy.json"),
		[]byte(`{ "listeners": { "plain": { "addr": ":4444" } } }`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir) // Go 1.24+; repo toolchain is >=1.25
	c, err := Load(nil)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.Plain.Addr != ":4444" {
		t.Errorf("Plain.Addr = %q, want \":4444\" from default-file pickup", c.Plain.Addr)
	}
}

func TestMissingDefaultConfigIsNotError(t *testing.T) {
	t.Chdir(t.TempDir()) // empty dir, no tn3270proxy.json
	c, err := Load(nil)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.Plain.Addr != ":2323" {
		t.Errorf("Plain.Addr = %q, want default", c.Plain.Addr)
	}
}

func TestUnknownConfigKeyIsError(t *testing.T) {
	p := writeConfig(t, `{ "listeners": { "tsl": { "enabled": true } } }`)
	_, err := Load([]string{"-config", p})
	if err == nil {
		t.Fatal("want error for unknown config key, got nil")
	}
}

func TestValidateNoListenerEnabled(t *testing.T) {
	p := writeConfig(t, `{ "listeners": { "plain": { "enabled": false } } }`)
	_, err := Load([]string{"-config", p})
	if err == nil {
		t.Fatal("want error when no listener is enabled, got nil")
	}
}

func TestValidateTLSWithoutCert(t *testing.T) {
	p := writeConfig(t, `{ "listeners": {
		"plain": { "enabled": false },
		"tls": { "enabled": true, "addr": ":3270" }
	} }`)
	_, err := Load([]string{"-config", p})
	if err == nil {
		t.Fatal("want error when tls enabled without cert/key, got nil")
	}
}
