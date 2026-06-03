package config

import "flag"

// Config holds runtime configuration for the proxy.
type Config struct {
	ListenAddr string // TCP address the proxy listens on
	DBPath     string // path to the SQLite database file
}

// Load parses the given argument slice (typically os.Args after the
// subcommand) into a Config, applying defaults.
func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	var c Config
	fs.StringVar(&c.ListenAddr, "listen", ":2323", "TCP listen address")
	fs.StringVar(&c.DBPath, "db", "tn3270proxy.db", "path to SQLite database file")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return c, nil
}
