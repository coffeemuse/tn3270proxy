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

// Package listen builds the proxy's network listeners (plaintext and TLS)
// from configuration. A TLS listener terminates TLS immediately on connect
// (not STARTTLS), which is what TN3270-over-TLS clients expect.
package listen

import (
	"crypto/tls"
	"fmt"
	"net"

	"github.com/coffeemuse/tn3270proxy/internal/config"
)

// Build returns one net.Listener per enabled transport in cfg. On any error
// it closes listeners already opened so no socket leaks.
func Build(cfg config.Config) ([]net.Listener, error) {
	var lns []net.Listener

	if cfg.Plain.Enabled {
		ln, err := net.Listen("tcp", cfg.Plain.Addr)
		if err != nil {
			closeAll(lns)
			return nil, fmt.Errorf("plain listener on %s: %w", cfg.Plain.Addr, err)
		}
		lns = append(lns, ln)
	}

	if cfg.TLS.Enabled {
		cert, err := tls.LoadX509KeyPair(cfg.TLS.Cert, cfg.TLS.Key)
		if err != nil {
			closeAll(lns)
			return nil, fmt.Errorf("load tls keypair: %w", err)
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		ln, err := net.Listen("tcp", cfg.TLS.Addr)
		if err != nil {
			closeAll(lns)
			return nil, fmt.Errorf("tls listener on %s: %w", cfg.TLS.Addr, err)
		}
		lns = append(lns, tls.NewListener(ln, tlsCfg))
	}

	if len(lns) == 0 {
		return nil, fmt.Errorf("no listeners enabled")
	}
	return lns, nil
}

func closeAll(lns []net.Listener) {
	for _, ln := range lns {
		ln.Close()
	}
}
