package config

import (
	"errors"
	"flag"
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

	// (config-file merge added in Task 2)
	_ = configPath

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
