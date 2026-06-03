package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
)

// Listener describes a plaintext TCP listener.
type Listener struct {
	Enabled bool
	Addr    string
}

// TLSListener describes a TLS-terminated TCP listener.
type TLSListener struct {
	Enabled bool
	Addr    string
	Cert    string
	Key     string
}

// Config holds runtime configuration for the proxy.
type Config struct {
	DBPath string
	Plain  Listener
	TLS    TLSListener
}

// fileConfig is the on-disk JSON shape. Pointer fields distinguish
// "absent in file" (leave default) from "present and zero".
type fileConfig struct {
	DB        *string `json:"db"`
	Listeners struct {
		Plain *struct {
			Enabled *bool   `json:"enabled"`
			Addr    *string `json:"addr"`
		} `json:"plain"`
		TLS *struct {
			Enabled *bool   `json:"enabled"`
			Addr    *string `json:"addr"`
			Cert    *string `json:"cert"`
			Key     *string `json:"key"`
		} `json:"tls"`
	} `json:"listeners"`
}

const (
	defaultDBPath     = "tn3270proxy.db"
	defaultPlainAddr  = ":2323"
	defaultConfigFile = "tn3270proxy.json"
)

func defaults() Config {
	return Config{
		DBPath: defaultDBPath,
		Plain:  Listener{Enabled: true, Addr: defaultPlainAddr},
		TLS:    TLSListener{Enabled: false},
	}
}

// Load parses args into a Config, applying precedence:
// defaults < config file < explicit flags.
func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to JSON config file (default tn3270proxy.json if present)")
	listenAddr := fs.String("listen", "", "plaintext TCP listen address (overrides config)")
	dbPath := fs.String("db", "", "path to SQLite database file (overrides config)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	cfg := defaults()

	path := defaultConfigFile
	explicit := set["config"]
	if explicit {
		path = *configPath
	}
	if err := mergeFile(&cfg, path, explicit); err != nil {
		return Config{}, err
	}

	if set["db"] {
		cfg.DBPath = *dbPath
	}
	if set["listen"] {
		cfg.Plain.Addr = *listenAddr
	}

	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func mergeFile(cfg *Config, path string, explicit bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return nil // default file is optional
		}
		return fmt.Errorf("read config %q: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var fc fileConfig
	if err := dec.Decode(&fc); err != nil {
		return fmt.Errorf("parse config %q: %w", path, err)
	}
	if fc.DB != nil {
		cfg.DBPath = *fc.DB
	}
	if p := fc.Listeners.Plain; p != nil {
		if p.Enabled != nil {
			cfg.Plain.Enabled = *p.Enabled
		}
		if p.Addr != nil {
			cfg.Plain.Addr = *p.Addr
		}
	}
	if tl := fc.Listeners.TLS; tl != nil {
		if tl.Enabled != nil {
			cfg.TLS.Enabled = *tl.Enabled
		}
		if tl.Addr != nil {
			cfg.TLS.Addr = *tl.Addr
		}
		if tl.Cert != nil {
			cfg.TLS.Cert = *tl.Cert
		}
		if tl.Key != nil {
			cfg.TLS.Key = *tl.Key
		}
	}
	return nil
}

func validate(cfg Config) error {
	if !cfg.Plain.Enabled && !cfg.TLS.Enabled {
		return errors.New("config: no listener enabled (enable plain and/or tls)")
	}
	if cfg.Plain.Enabled && cfg.Plain.Addr == "" {
		return errors.New("config: plain listener enabled but addr is empty")
	}
	if cfg.TLS.Enabled {
		if cfg.TLS.Addr == "" {
			return errors.New("config: tls listener enabled but addr is empty")
		}
		if cfg.TLS.Cert == "" || cfg.TLS.Key == "" {
			return errors.New("config: tls listener enabled but cert/key not set")
		}
	}
	return nil
}
