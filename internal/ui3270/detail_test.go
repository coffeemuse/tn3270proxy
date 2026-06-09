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
	"context"
	"testing"
)

func baseDetailCfg(onActStatus string, refresh bool, acted *bool) DetailConfig {
	return DetailConfig{
		View:       DetailView{Title: "SESSION DETAIL", PFHelp: "PF11=Disconnect   PF3=Back"},
		ActPF:      11,
		DonePFHelp: "PF3=Back",
		Confirm:    func() (string, string) { return "CONFIRM DISCONNECT BOB - PRESS PF11 AGAIN", "" },
		OnAct: func(context.Context) (string, bool) {
			*acted = true
			return onActStatus, refresh
		},
	}
}

func TestRunDetail_TwoPressCommit(t *testing.T) {
	acted := false
	cfg := baseDetailCfg("DISCONNECTED", true, &acted)
	r := &scriptRenderer{detActs: []ListAction{{PF: 11}, {PF: 11}, {PF: 3}}}
	refresh, err := RunDetail(context.Background(), r, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !acted {
		t.Error("OnAct must run on the second PF11")
	}
	if !refresh {
		t.Error("refresh must propagate from OnAct")
	}
	if r.dets[1].Message != "CONFIRM DISCONNECT BOB - PRESS PF11 AGAIN" {
		t.Errorf("prompt = %q", r.dets[1].Message)
	}
	if r.dets[2].Message != "DISCONNECTED" {
		t.Errorf("status = %q", r.dets[2].Message)
	}
	if r.dets[2].PFHelp != "PF3=Back" {
		t.Errorf("done PFHelp = %q, want PF3=Back", r.dets[2].PFHelp)
	}
}

func TestRunDetail_VetoNeverCommits(t *testing.T) {
	acted := false
	cfg := baseDetailCfg("DISCONNECTED", true, &acted)
	cfg.Confirm = func() (string, string) { return "", "CANNOT DISCONNECT YOUR OWN SESSION" }
	r := &scriptRenderer{detActs: []ListAction{{PF: 11}, {PF: 3}}}
	refresh, err := RunDetail(context.Background(), r, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if acted {
		t.Error("OnAct must not run after a veto")
	}
	if refresh {
		t.Error("a vetoed action must not request a refresh")
	}
	if r.dets[1].Message != "CANNOT DISCONNECT YOUR OWN SESSION" {
		t.Errorf("veto message = %q", r.dets[1].Message)
	}
}

func TestRunDetail_PF3WithoutActionDoesNotRefresh(t *testing.T) {
	acted := false
	cfg := baseDetailCfg("DISCONNECTED", true, &acted)
	r := &scriptRenderer{detActs: []ListAction{{PF: 3}}}
	refresh, err := RunDetail(context.Background(), r, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if acted || refresh {
		t.Errorf("plain PF3 must neither act nor refresh; acted=%v refresh=%v", acted, refresh)
	}
}

func TestRunDetail_ActionDisarmsAfterCommit(t *testing.T) {
	acted := false
	cfg := baseDetailCfg("DISCONNECTED", true, &acted)
	rec := &recordPFRenderer{detActs: []ListAction{{PF: 11}, {PF: 11}, {PF: 3}}}
	if _, err := RunDetail(context.Background(), rec, cfg); err != nil {
		t.Fatal(err)
	}
	if got := rec.actPFs; len(got) != 3 || got[0] != 11 || got[1] != 11 || got[2] != 0 {
		t.Errorf("actPF sequence = %v, want [11 11 0]", got)
	}
}

// recordPFRenderer records the actPF passed to each DetailAct call.
type recordPFRenderer struct {
	detActs []ListAction
	actPFs  []int
}

func (r *recordPFRenderer) List(ListView) (ListAction, error)         { panic("unused") }
func (r *recordPFRenderer) Form(FormView) (FormAction, error)         { panic("unused") }
func (r *recordPFRenderer) Snapshot(SnapshotView) (ListAction, error) { panic("unused") }
func (r *recordPFRenderer) Detail(DetailView) error                   { return nil }
func (r *recordPFRenderer) DetailAct(_ DetailView, actPF int) (ListAction, error) {
	r.actPFs = append(r.actPFs, actPF)
	a := r.detActs[0]
	r.detActs = r.detActs[1:]
	return a, nil
}
