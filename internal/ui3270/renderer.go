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

package ui3270

import (
	"net"
	"slices"

	"github.com/racingmars/go3270"
)

// silentAIDs are data-less attention keys that must be silent no-ops on these
// screens: go3270 otherwise flashes "<key>: unknown key" and re-presents
// without returning. They MUST be passed as exit keys so the present-loop sees
// them. (PA3 is also the bridge escape key, but bridging is handled elsewhere;
// this only runs on the proxy's own widget screens.)
var silentAIDs = []go3270.AID{go3270.AIDPA1, go3270.AIDPA2, go3270.AIDPA3, go3270.AIDClear}

// listExitKeys are the AIDs RunList acts on; PA-keys/Clear are appended as
// silent exits. PF4 is always an exit key even when Add is nil (RunList then
// ignores it), preserving the historical behavior.
var listExitKeys = []go3270.AID{go3270.AIDPF3, go3270.AIDPF4, go3270.AIDPF7, go3270.AIDPF8}

func isSilent(aid go3270.AID) bool { return slices.Contains(silentAIDs, aid) }

func withSilentExits(keys []go3270.AID) []go3270.AID {
	out := make([]go3270.AID, 0, len(keys)+len(silentAIDs))
	out = append(out, keys...)
	return append(out, silentAIDs...)
}

// present calls fn in a loop, silently re-presenting on PA1/PA2/PA3/Clear.
func present(fn func() (go3270.Response, error)) (go3270.Response, error) {
	for {
		resp, err := fn()
		if err != nil || !isSilent(resp.AID) {
			return resp, err
		}
	}
}

type go3270Renderer struct {
	conn     net.Conn
	dev      go3270.DevInfo
	codepage go3270.Codepage
	rows     int
}

// NewGo3270Renderer returns a Renderer that paints via go3270 over conn. dev
// and codepage come from the negotiated terminal (dev nil => 24x80 fallback).
func NewGo3270Renderer(conn net.Conn, dev go3270.DevInfo, codepage go3270.Codepage, rows int) Renderer {
	return &go3270Renderer{conn: conn, dev: dev, codepage: codepage, rows: rows}
}

func (g *go3270Renderer) call(screen go3270.Screen, exit []go3270.AID, cur Cursor) (go3270.Response, error) {
	return present(func() (go3270.Response, error) {
		return go3270.HandleScreenAlt(
			screen, nil, map[string]string{},
			[]go3270.AID{go3270.AIDEnter},
			withSilentExits(exit),
			fieldError, cur.Row, cur.Col, g.conn, g.dev, g.codepage,
		)
	})
}

func (g *go3270Renderer) List(v ListView) (ListAction, error) {
	screen, cur := buildListScreen(g.rows, v)
	resp, err := g.call(screen, listExitKeys, cur)
	if err != nil {
		return ListAction{}, err
	}
	return listAction(resp, len(v.Rows)), nil
}

func (g *go3270Renderer) Form(v FormView) (FormAction, error) {
	screen, cur := buildFormScreen(g.rows, v)
	resp, err := g.call(screen, []go3270.AID{go3270.AIDPF3}, cur)
	if err != nil {
		return FormAction{}, err
	}
	return formAction(resp, v.Fields), nil
}

var snapshotExitKeys = []go3270.AID{go3270.AIDPF3, go3270.AIDPF7, go3270.AIDPF8}

func (g *go3270Renderer) Snapshot(v SnapshotView) (ListAction, error) {
	screen, cur := buildSnapshotScreen(g.rows, v)
	resp, err := g.call(screen, snapshotExitKeys, cur)
	if err != nil {
		return ListAction{}, err
	}
	return listAction(resp, len(v.Rows)), nil
}

func (g *go3270Renderer) Detail(v DetailView) error {
	screen, cur := buildDetailScreen(g.rows, v)
	_, err := g.call(screen, []go3270.AID{go3270.AIDPF3}, cur)
	return err
}

func (g *go3270Renderer) DetailAct(v DetailView, actPF int) (ListAction, error) {
	screen, cur := buildDetailScreen(g.rows, v)
	exit := []go3270.AID{go3270.AIDPF3}
	if a, ok := pfAID(actPF); ok {
		exit = append(exit, a)
	}
	resp, err := g.call(screen, exit, cur)
	if err != nil {
		return ListAction{}, err
	}
	return detailAction(resp, actPF), nil
}

var editorExitKeys = []go3270.AID{go3270.AIDPF3, go3270.AIDPF7, go3270.AIDPF8, go3270.AIDPF12}

func (g *go3270Renderer) Editor(v EditorView) (EditorAction, error) {
	screen, cur := buildEditorScreen(g.rows, v)
	resp, err := g.call(screen, editorExitKeys, cur)
	if err != nil {
		return EditorAction{}, err
	}
	return editorAction(resp, len(v.Lines)), nil
}
