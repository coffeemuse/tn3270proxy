package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
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

// Limits bounds per-connection lifetime and concurrency on the public
// listeners (slowloris/DoS hardening, GH issue #1).
type Limits struct {
	PreAuthIdle time.Duration // idle deadline before authentication
	Idle        time.Duration // idle deadline after authentication (incl. bridged sessions)
	MaxConns    int           // global concurrent-connection cap
	MaxPerIP    int           // per-client-IP cap; 0 disables
}

// Config holds runtime configuration for the proxy.
type Config struct {
	DBPath string
	Plain  Listener
	TLS    TLSListener
	Limits Limits
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
	Limits *struct {
		PreAuthIdle *string `json:"pre_auth_idle"` // Go duration string, e.g. "2m"
		Idle        *string `json:"idle"`
		MaxConns    *int    `json:"max_conns"`
		MaxPerIP    *int    `json:"max_per_ip"`
	} `json:"limits"`
}

const (
	defaultDBPath      = "tn3270proxy.db"
	defaultPlainAddr   = ":2323"
	defaultConfigFile  = "tn3270proxy.json"
	defaultPreAuthIdle = 2 * time.Minute
	defaultIdle        = 30 * time.Minute
	defaultMaxConns    = 512
	defaultMaxPerIP    = 16
)

func defaults() Config {
	return Config{
		DBPath: defaultDBPath,
		Plain:  Listener{Enabled: true, Addr: defaultPlainAddr},
		TLS:    TLSListener{Enabled: false},
		Limits: Limits{
			PreAuthIdle: defaultPreAuthIdle,
			Idle:        defaultIdle,
			MaxConns:    defaultMaxConns,
			MaxPerIP:    defaultMaxPerIP,
		},
	}
}

// Load parses args into a Config, applying precedence:
// defaults < config file < explicit flags.
func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to JSON config file (default tn3270proxy.json if present)")
	listenAddr := fs.String("listen", "", "plaintext TCP listen address (overrides config)")
	dbPath := fs.String("db", "", "path to SQLite database file (overrides config)")
	preAuthIdle := fs.Duration("pre-auth-idle", 0, "idle timeout before login (overrides config)")
	idle := fs.Duration("idle", 0, "idle timeout after login, incl. bridged sessions (overrides config)")
	maxConns := fs.Int("max-conns", 0, "max concurrent connections (overrides config)")
	maxPerIP := fs.Int("max-per-ip", -1, "max concurrent connections per client IP, 0 disables (overrides config)")
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
		if !cfg.Plain.Enabled {
			return Config{}, errors.New("config: -listen given but the plain listener is disabled by the config file; set listeners.plain.enabled=true or drop -listen")
		}
		cfg.Plain.Addr = *listenAddr
	}
	if set["pre-auth-idle"] {
		cfg.Limits.PreAuthIdle = *preAuthIdle
	}
	if set["idle"] {
		cfg.Limits.Idle = *idle
	}
	if set["max-conns"] {
		cfg.Limits.MaxConns = *maxConns
	}
	if set["max-per-ip"] {
		cfg.Limits.MaxPerIP = *maxPerIP
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
	if l := fc.Limits; l != nil {
		if l.PreAuthIdle != nil {
			d, err := time.ParseDuration(*l.PreAuthIdle)
			if err != nil {
				return fmt.Errorf("config: limits.pre_auth_idle: %w", err)
			}
			cfg.Limits.PreAuthIdle = d
		}
		if l.Idle != nil {
			d, err := time.ParseDuration(*l.Idle)
			if err != nil {
				return fmt.Errorf("config: limits.idle: %w", err)
			}
			cfg.Limits.Idle = d
		}
		if l.MaxConns != nil {
			cfg.Limits.MaxConns = *l.MaxConns
		}
		if l.MaxPerIP != nil {
			cfg.Limits.MaxPerIP = *l.MaxPerIP
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
	if cfg.Limits.PreAuthIdle <= 0 {
		return errors.New("config: limits.pre_auth_idle must be positive")
	}
	if cfg.Limits.Idle <= 0 {
		return errors.New("config: limits.idle must be positive")
	}
	if cfg.Limits.MaxConns <= 0 {
		return errors.New("config: limits.max_conns must be positive")
	}
	if cfg.Limits.MaxPerIP < 0 {
		return errors.New("config: limits.max_per_ip must be >= 0 (0 disables)")
	}
	return nil
}
