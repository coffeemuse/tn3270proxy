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
	"testing"

	"github.com/racingmars/go3270"
)

func TestListActionPFKeys(t *testing.T) {
	for _, c := range []struct {
		aid    go3270.AID
		wantPF int
	}{{go3270.AIDPF3, 3}, {go3270.AIDPF4, 4}, {go3270.AIDPF7, 7}, {go3270.AIDPF8, 8}} {
		if got := listAction(go3270.Response{AID: c.aid}, 0); got.PF != c.wantPF {
			t.Errorf("AID %v → PF %d, want %d", c.aid, got.PF, c.wantPF)
		}
	}
}

func TestListActionFirstCmdWins(t *testing.T) {
	resp := go3270.Response{AID: go3270.AIDEnter, Values: map[string]string{"cmd0": " ", "cmd1": "d"}}
	got := listAction(resp, 2)
	if got.Cmd != 'D' || got.Row != 1 {
		t.Errorf("got Cmd=%q Row=%d, want 'D' row 1", got.Cmd, got.Row)
	}
}

func TestFormActionTrimsVisibleNotHidden(t *testing.T) {
	resp := go3270.Response{AID: go3270.AIDEnter, Values: map[string]string{"u": " bob ", "p": " pw "}}
	got := formAction(resp, []FormField{{Name: "u"}, {Name: "p", Hidden: true}})
	if got.Values["u"] != "bob" || got.Values["p"] != " pw " {
		t.Errorf("got %q / %q, want \"bob\" / \" pw \"", got.Values["u"], got.Values["p"])
	}
}

func TestFormActionCancel(t *testing.T) {
	if got := formAction(go3270.Response{AID: go3270.AIDPF3}, nil); !got.Cancel {
		t.Error("PF3 should map to Cancel")
	}
}

func TestDetailAction(t *testing.T) {
	if got := detailAction(go3270.Response{AID: go3270.AIDPF3}, 11); got != (ListAction{PF: 3}) {
		t.Errorf("PF3 → %+v, want {PF:3}", got)
	}
	if got := detailAction(go3270.Response{AID: go3270.AIDPF11}, 11); got != (ListAction{PF: 11}) {
		t.Errorf("PF11 with actPF=11 → %+v, want {PF:11}", got)
	}
	if got := detailAction(go3270.Response{AID: go3270.AIDPF11}, 0); got != (ListAction{}) {
		t.Errorf("PF11 with actPF=0 → %+v, want {}", got)
	}
	if got := detailAction(go3270.Response{AID: go3270.AIDEnter}, 11); got != (ListAction{}) {
		t.Errorf("Enter → %+v, want {}", got)
	}
}
