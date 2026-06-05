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

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestListenFlag(t *testing.T) {
	cases := []struct {
		name     string
		cfgJSON  string // empty = no config file
		args     []string
		wantErr  bool
		wantAddr string
	}{
		{
			name: "tls-only file plus -listen returns error",
			cfgJSON: `{ "listeners": {
				"plain": { "enabled": false },
				"tls": { "enabled": true, "addr": ":3270", "cert": "c.pem", "key": "k.pem" }
			} }`,
			args:    []string{"-listen", ":2323"},
			wantErr: true,
		},
		{
			name:     "plain-enabled file plus -listen overrides addr",
			cfgJSON:  `{ "listeners": { "plain": { "enabled": true, "addr": ":111" } } }`,
			args:     []string{"-listen", ":222"},
			wantErr:  false,
			wantAddr: ":222",
		},
		{
			name:     "no config file plus -listen uses the given addr",
			cfgJSON:  "",
			args:     []string{"-listen", ":9000"},
			wantErr:  false,
			wantAddr: ":9000",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := tc.args
			if tc.cfgJSON != "" {
				p := writeConfig(t, tc.cfgJSON)
				args = append([]string{"-config", p}, args...)
			} else {
				t.Chdir(t.TempDir()) // empty dir — no default config file
			}
			c, err := Load(args)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load error: %v", err)
			}
			if c.Plain.Addr != tc.wantAddr {
				t.Errorf("Plain.Addr = %q, want %q", c.Plain.Addr, tc.wantAddr)
			}
			if !c.Plain.Enabled {
				t.Errorf("Plain.Enabled = false, want true")
			}
		})
	}
}

func TestValidateNoListenerEnabled(t *testing.T) {
	p := writeConfig(t, `{ "listeners": { "plain": { "enabled": false } } }`)
	_, err := Load([]string{"-config", p})
	if err == nil {
		t.Fatal("want error when no listener is enabled, got nil")
	}
}

func TestLimitsDefaults(t *testing.T) {
	t.Chdir(t.TempDir()) // no config file
	c, err := Load(nil)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.Limits.PreAuthIdle != 2*time.Minute {
		t.Errorf("PreAuthIdle = %v, want 2m", c.Limits.PreAuthIdle)
	}
	if c.Limits.Idle != 30*time.Minute {
		t.Errorf("Idle = %v, want 30m", c.Limits.Idle)
	}
	if c.Limits.MaxConns != 512 {
		t.Errorf("MaxConns = %d, want 512", c.Limits.MaxConns)
	}
	if c.Limits.MaxPerIP != 16 {
		t.Errorf("MaxPerIP = %d, want 16", c.Limits.MaxPerIP)
	}
	if c.Limits.PreAuthMax != 5*time.Minute {
		t.Errorf("PreAuthMax = %v, want 5m", c.Limits.PreAuthMax)
	}
	if c.Limits.BridgeIdleExempt {
		t.Errorf("BridgeIdleExempt = true, want false")
	}
}

func TestLimitsFromFile(t *testing.T) {
	p := writeConfig(t, `{ "limits": {
		"pre_auth_idle": "30s",
		"idle": "1h",
		"max_conns": 100,
		"max_per_ip": 0
	} }`)
	c, err := Load([]string{"-config", p})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.Limits.PreAuthIdle != 30*time.Second {
		t.Errorf("PreAuthIdle = %v, want 30s", c.Limits.PreAuthIdle)
	}
	if c.Limits.Idle != time.Hour {
		t.Errorf("Idle = %v, want 1h", c.Limits.Idle)
	}
	if c.Limits.MaxConns != 100 {
		t.Errorf("MaxConns = %d, want 100", c.Limits.MaxConns)
	}
	if c.Limits.MaxPerIP != 0 {
		t.Errorf("MaxPerIP = %d, want 0", c.Limits.MaxPerIP)
	}
}

func TestLimitsPartialFileKeepsDefaults(t *testing.T) {
	p := writeConfig(t, `{ "limits": { "max_conns": 64 } }`)
	c, err := Load([]string{"-config", p})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.Limits.MaxConns != 64 {
		t.Errorf("MaxConns = %d, want 64", c.Limits.MaxConns)
	}
	if c.Limits.PreAuthIdle != 2*time.Minute || c.Limits.Idle != 30*time.Minute || c.Limits.MaxPerIP != 16 {
		t.Errorf("unset limits changed: %+v", c.Limits)
	}
}

func TestLimitsFlagsOverrideFile(t *testing.T) {
	p := writeConfig(t, `{ "limits": { "idle": "1h", "max_conns": 100 } }`)
	c, err := Load([]string{"-config", p,
		"-idle", "45m", "-pre-auth-idle", "90s", "-max-conns", "200", "-max-per-ip", "4"})
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.Limits.PreAuthIdle != 90*time.Second {
		t.Errorf("PreAuthIdle = %v, want 90s", c.Limits.PreAuthIdle)
	}
	if c.Limits.Idle != 45*time.Minute {
		t.Errorf("Idle = %v, want 45m", c.Limits.Idle)
	}
	if c.Limits.MaxConns != 200 {
		t.Errorf("MaxConns = %d, want 200", c.Limits.MaxConns)
	}
	if c.Limits.MaxPerIP != 4 {
		t.Errorf("MaxPerIP = %d, want 4", c.Limits.MaxPerIP)
	}
}

func TestLimitsBadDurationIsError(t *testing.T) {
	p := writeConfig(t, `{ "limits": { "idle": "soon" } }`)
	if _, err := Load([]string{"-config", p}); err == nil {
		t.Fatal("want error for unparseable idle duration, got nil")
	}
}

func TestLimitsValidation(t *testing.T) {
	cases := []struct{ name, body string }{
		{"zero idle", `{ "limits": { "idle": "0s" } }`},
		{"negative pre_auth_idle", `{ "limits": { "pre_auth_idle": "-1m" } }`},
		{"zero max_conns", `{ "limits": { "max_conns": 0 } }`},
		{"negative max_per_ip", `{ "limits": { "max_per_ip": -1 } }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := writeConfig(t, tc.body)
			if _, err := Load([]string{"-config", p}); err == nil {
				t.Fatalf("want error for %s, got nil", tc.name)
			}
		})
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

func TestLoadDefaultsNewLimits(t *testing.T) {
	cfg, err := Load([]string{"-config", writeConfig(t, `{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Limits.PreAuthMax != 5*time.Minute {
		t.Errorf("PreAuthMax default = %v, want 5m", cfg.Limits.PreAuthMax)
	}
	if cfg.Limits.BridgeIdleExempt {
		t.Error("BridgeIdleExempt default = true, want false")
	}
}

// TestTrustedCIDRsConfigKeyIsGone confirms that a config file with the old
// trusted_cidrs key now fails fast via DisallowUnknownFields — the intended
// "it moved" signal (trusted networks are now DB-managed via the admin UI).
func TestTrustedCIDRsConfigKeyIsGone(t *testing.T) {
	path := writeConfig(t, `{"limits":{"trusted_cidrs":["10.0.0.0/24"]}}`)
	if _, err := Load([]string{"-config", path}); err == nil {
		t.Fatal("expected error for removed trusted_cidrs key, got nil")
	}
}

func TestLoadBridgeIdle(t *testing.T) {
	for _, c := range []struct {
		val        string
		wantExempt bool
		wantErr    bool
	}{
		{`"disconnect"`, false, false},
		{`"exempt"`, true, false},
		{`"bogus"`, false, true},
	} {
		path := writeConfig(t, `{"limits":{"bridge_idle":`+c.val+`}}`)
		cfg, err := Load([]string{"-config", path})
		if c.wantErr {
			if err == nil {
				t.Errorf("bridge_idle=%s: expected error", c.val)
			}
			continue
		}
		if err != nil {
			t.Fatalf("bridge_idle=%s: %v", c.val, err)
		}
		if cfg.Limits.BridgeIdleExempt != c.wantExempt {
			t.Errorf("bridge_idle=%s: exempt=%v, want %v", c.val, cfg.Limits.BridgeIdleExempt, c.wantExempt)
		}
	}
}

func TestLoadPreAuthMaxFlagOverrides(t *testing.T) {
	path := writeConfig(t, `{"limits":{"pre_auth_max":"5m"}}`)
	cfg, err := Load([]string{"-config", path, "-pre-auth-max", "90s"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Limits.PreAuthMax != 90*time.Second {
		t.Errorf("PreAuthMax = %v, want 90s (flag overrides file)", cfg.Limits.PreAuthMax)
	}
}
