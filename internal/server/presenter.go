package server

import (
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
	screen, rules := screens.LoginScreen(errMsg)
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

func (go3270Presenter) Menu(conn net.Conn, svcs []store.Service, errMsg string) (*store.Service, bool, error) {
	for {
		screen, mapping := screens.MenuScreen(svcs, errMsg)
		resp, err := go3270.HandleScreen(
			screen, nil, map[string]string{},
			[]go3270.AID{go3270.AIDEnter},
			[]go3270.AID{go3270.AIDPF3},
			screens.FieldError, 19, 8, conn,
		)
		if err != nil {
			return nil, false, err
		}
		if resp.AID == go3270.AIDPF3 {
			return nil, true, nil
		}
		key := strings.TrimSpace(resp.Values[screens.FieldSelection])
		if svc, ok := mapping[key]; ok {
			return &svc, false, nil
		}
		errMsg = "Invalid selection: " + key
	}
}

// realBridger adapts bridge.Bridge to the Bridger interface.
type realBridger struct{}

func (realBridger) Bridge(client net.Conn, addr, termType string, escapeAID byte) (bridge.Cause, error) {
	return bridge.Bridge(client, addr, termType, escapeAID, nil)
}
