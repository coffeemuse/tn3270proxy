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

// Package config loads the gateway's deployment configuration: which
// listeners to open, connection limits, logging, the database path, and the
// MFA master key. Precedence is defaults < JSON config file < command-line
// flags, with the TN3270PROXY_MFA_KEY environment variable overriding the
// file for the MFA key only.
//
// The config file (tn3270proxy.json in the working directory unless -config
// says otherwise) is the deployment surface; runtime parameters that
// operators change day-to-day (MOTD, branding, system ID, auth throttling,
// trusted networks) live in the database and are edited through the 3270
// admin UI instead.
package config

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coffeemuse/tn3270proxy/internal/logging"
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
	PreAuthIdle      time.Duration // idle deadline before authentication
	Idle             time.Duration // idle deadline after authentication (incl. bridges unless BridgeIdleExempt)
	MaxConns         int           // global concurrent-connection cap
	MaxPerIP         int           // per-client-IP cap; 0 disables
	PreAuthMax       time.Duration // absolute deadline to authenticate (GH #18)
	BridgeIdleExempt bool          // true → no idle timeout during an active bridge
}

// Log holds logging configuration.
type Log struct {
	Level string // "error", "warn", "info", "debug"; default "info"
	File  string // when non-empty, JSON output is written here in addition to stderr
}

// EnvMFAKey is the environment variable holding the base64-encoded 32-byte
// AES-256 master key for MFA secret encryption. It takes precedence over the
// config file.
const EnvMFAKey = "TN3270PROXY_MFA_KEY"

// MFAConfig holds the resolved MFA master key. Key is nil when MFA is not
// configured (no enrolled users may then exist — enforced at startup).
type MFAConfig struct {
	Key []byte // 32-byte AES-256 key, or nil
}

// Config holds runtime configuration for the proxy.
type Config struct {
	DBPath string
	Plain  Listener
	TLS    TLSListener
	Limits Limits
	Log    Log
	MFA    MFAConfig
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
		PreAuthMax  *string `json:"pre_auth_max"` // Go duration string, e.g. "5m"
		BridgeIdle  *string `json:"bridge_idle"`  // "disconnect" (default) | "exempt"
	} `json:"limits"`
	Log *struct {
		Level *string `json:"level"` // "error", "warn", "info", "debug"
		File  *string `json:"file"`  // path for JSON log file output
	} `json:"log"`
	MFA *struct {
		Key     *string `json:"key"`      // base64 of 32 bytes
		KeyFile *string `json:"key_file"` // path to a file containing the base64 key
	} `json:"mfa"`
}

const (
	defaultDBPath      = "tn3270proxy.db"
	defaultPlainAddr   = ":2323"
	defaultConfigFile  = "tn3270proxy.json"
	defaultPreAuthIdle = 2 * time.Minute
	defaultIdle        = 30 * time.Minute
	defaultMaxConns    = 512
	defaultMaxPerIP    = 16
	defaultPreAuthMax  = 5 * time.Minute
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
			PreAuthMax:  defaultPreAuthMax,
		},
		Log: Log{Level: "info"},
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
	preAuthMax := fs.Duration("pre-auth-max", 0, "absolute deadline to authenticate (overrides config)")
	logLevel := fs.String("log-level", "", "log level: error, warn, info, debug (overrides config)")
	logFile := fs.String("log-file", "", "write JSON logs to this file in addition to stderr (overrides config)")
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
		// -listen only re-addresses an enabled plain listener; it must never
		// silently re-enable plaintext on a gateway whose config file
		// deliberately disabled it, so a conflict is fatal.
		if !cfg.Plain.Enabled {
			return Config{}, errors.New("config: -listen given but the plain listener is disabled by the config file; set listeners.plain.enabled=true or drop -listen")
		}
		cfg.Plain.Addr = *listenAddr
	}
	if set["pre-auth-idle"] {
		cfg.Limits.PreAuthIdle = *preAuthIdle
	}
	if set["pre-auth-max"] {
		cfg.Limits.PreAuthMax = *preAuthMax
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
	if set["log-level"] {
		cfg.Log.Level = *logLevel
	}
	if set["log-file"] {
		cfg.Log.File = *logFile
	}

	if env := os.Getenv(EnvMFAKey); env != "" {
		k, err := decodeMFAKey(env)
		if err != nil {
			return Config{}, err
		}
		cfg.MFA.Key = k
	}

	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// decodeMFAKey decodes a base64 master key and enforces the 32-byte length.
func decodeMFAKey(b64 string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, fmt.Errorf("config: mfa key: not valid base64: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("config: mfa key: must decode to 32 bytes, got %d", len(raw))
	}
	return raw, nil
}

// mergeFile overlays the JSON file at path onto cfg. A missing file is only an
// error when the user named it explicitly via -config; the default
// tn3270proxy.json is optional. Unknown keys are rejected so a typo (or a key
// removed in a newer version, e.g. the old limits.trusted_cidrs) fails fast
// instead of being silently ignored.
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
	if lg := fc.Log; lg != nil {
		if lg.Level != nil {
			cfg.Log.Level = *lg.Level
		}
		if lg.File != nil {
			cfg.Log.File = *lg.File
		}
	}
	if m := fc.MFA; m != nil {
		switch {
		case m.Key != nil && *m.Key != "":
			k, err := decodeMFAKey(*m.Key)
			if err != nil {
				return err
			}
			cfg.MFA.Key = k
		case m.KeyFile != nil && *m.KeyFile != "":
			raw, err := os.ReadFile(*m.KeyFile)
			if err != nil {
				return fmt.Errorf("config: mfa key_file: %w", err)
			}
			k, err := decodeMFAKey(string(raw))
			if err != nil {
				return err
			}
			cfg.MFA.Key = k
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
		if l.PreAuthMax != nil {
			d, err := time.ParseDuration(*l.PreAuthMax)
			if err != nil {
				return fmt.Errorf("config: limits.pre_auth_max: %w", err)
			}
			cfg.Limits.PreAuthMax = d
		}
		if l.BridgeIdle != nil {
			switch *l.BridgeIdle {
			case "disconnect":
				cfg.Limits.BridgeIdleExempt = false
			case "exempt":
				cfg.Limits.BridgeIdleExempt = true
			default:
				return fmt.Errorf("config: limits.bridge_idle: %q (want \"disconnect\" or \"exempt\")", *l.BridgeIdle)
			}
		}
	}
	return nil
}

// validate enforces invariants on the merged result (not on any single
// source): at least one listener, complete TLS material, positive limits.
func validate(cfg Config) error {
	if _, err := logging.ParseLevel(cfg.Log.Level); err != nil {
		return fmt.Errorf("config: log.level: %q (want \"error\", \"warn\", \"info\", or \"debug\")", cfg.Log.Level)
	}
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
	if cfg.Limits.PreAuthMax <= 0 {
		return errors.New("config: limits.pre_auth_max must be positive")
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
