package server

import (
	"crypto/tls"
	"net"
	"strings"

	"github.com/CoffeeMuse/tn3270proxy/internal/bridge"
	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/racingmars/go3270"
)

// go3270Presenter renders screens using the go3270 library over a raw conn.
type go3270Presenter struct{}

func (go3270Presenter) Negotiate(conn net.Conn) (string, error) {
	dev, err := go3270.NegotiateTelnet(conn)
	if err != nil {
		return "", err
	}
	return dev.TerminalType(), nil
}

func (go3270Presenter) Login(conn net.Conn, errMsg string) (string, string, bool, error) {
	screen, rules := screens.LoginScreen(screens.DefaultGeometry, errMsg)
	resp, err := go3270.HandleScreen(
		screen, rules, map[string]string{},
		[]go3270.AID{go3270.AIDEnter},
		[]go3270.AID{go3270.AIDPF3},
		screens.FieldError, 3, 17, conn,
	)
	if err != nil {
		return "", "", false, err
	}
	if resp.AID == go3270.AIDPF3 {
		return "", "", true, nil
	}
	return strings.TrimSpace(resp.Values[screens.FieldUsername]),
		resp.Values[screens.FieldPassword], false, nil
}

func (go3270Presenter) Menu(conn net.Conn, svcs []store.Service, admin bool, errMsg string) (*store.Service, bool, bool, error) {
	for {
		screen, mapping := screens.MenuScreen(svcs, admin, errMsg)
		resp, err := go3270.HandleScreen(
			screen, nil, map[string]string{},
			[]go3270.AID{go3270.AIDEnter},
			[]go3270.AID{go3270.AIDPF3},
			screens.FieldError, 19, 8, conn,
		)
		if err != nil {
			return nil, false, false, err
		}
		if resp.AID == go3270.AIDPF3 {
			return nil, false, true, nil
		}
		key := strings.ToUpper(strings.TrimSpace(resp.Values[screens.FieldSelection]))
		if admin && key == "A" {
			return nil, true, false, nil
		}
		if svc, ok := mapping[key]; ok {
			return &svc, false, false, nil
		}
		errMsg = "Invalid selection: " + key
	}
}

// realBridger adapts bridge.Bridge to the Bridger interface.
type realBridger struct{}

func (realBridger) Bridge(client net.Conn, addr, termType string, escapeAID byte, btls BackendTLS) (bridge.Cause, error) {
	return bridge.Bridge(client, addr, termType, escapeAID, backendTLSConfig(addr, btls))
}

// backendTLSConfig builds the dial-time tls.Config for a backend, or nil for a
// plaintext dial. ServerName is always the configured host; verification uses
// the system root store (browser-like). Verify=false encrypts without
// authenticating (for internal hosts with self-signed certs).
func backendTLSConfig(addr string, btls BackendTLS) *tls.Config {
	if !btls.Enabled {
		return nil
	}
	// addr is always net.JoinHostPort output, so SplitHostPort cannot fail;
	// the fallback sets an invalid ServerName that TLS will reject at
	// handshake (loud failure, never a silent verification skip).
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         host,
		InsecureSkipVerify: !btls.Verify,
	}
}
