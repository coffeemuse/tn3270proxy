package config

import "testing"

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
