package server

import (
	"testing"

	"github.com/CoffeeMuse/tn3270proxy/internal/screens"
	"github.com/racingmars/go3270"
)

func TestListActionFromResponse(t *testing.T) {
	cases := []struct {
		name   string
		aid    go3270.AID
		values map[string]string
		nRows  int
		want   AdminListAction
	}{
		{"pf3", go3270.AIDPF3, nil, 2, AdminListAction{PF: 3}},
		{"pf4", go3270.AIDPF4, nil, 2, AdminListAction{PF: 4}},
		{"pf7", go3270.AIDPF7, nil, 2, AdminListAction{PF: 7}},
		{"pf8", go3270.AIDPF8, nil, 2, AdminListAction{PF: 8}},
		{"plain enter", go3270.AIDEnter, map[string]string{"cmd0": " ", "cmd1": ""}, 2, AdminListAction{}},
		{"line cmd lowercased", go3270.AIDEnter, map[string]string{"cmd0": "", "cmd1": "d"}, 2, AdminListAction{Cmd: 'D', Row: 1}},
		{"first cmd wins", go3270.AIDEnter, map[string]string{"cmd0": "S", "cmd1": "D"}, 2, AdminListAction{Cmd: 'S', Row: 0}},
	}
	for _, c := range cases {
		got := listActionFromResponse(go3270.Response{AID: c.aid, Values: c.values}, c.nRows)
		if got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestFormActionFromResponse(t *testing.T) {
	fields := []screens.AdminFormField{
		{Name: screens.FieldUsername},
		{Name: screens.FieldPassword, Hidden: true},
	}
	got := formActionFromResponse(go3270.Response{AID: go3270.AIDPF3}, fields)
	if !got.Cancel {
		t.Errorf("PF3 should cancel: %+v", got)
	}
	got = formActionFromResponse(go3270.Response{
		AID:    go3270.AIDEnter,
		Values: map[string]string{screens.FieldUsername: " alice ", screens.FieldPassword: " p "},
	}, fields)
	if got.Values[screens.FieldUsername] != "alice" {
		t.Errorf("visible fields are trimmed: %q", got.Values[screens.FieldUsername])
	}
	if got.Values[screens.FieldPassword] != " p " {
		t.Errorf("hidden fields are NOT trimmed: %q", got.Values[screens.FieldPassword])
	}
}
