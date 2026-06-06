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

package server

import (
	"crypto/tls"
	"testing"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

func TestClassifyMenuSubmit(t *testing.T) {
	svc1 := store.Service{Name: "SVC-A", Host: "h1", Port: 23}
	svc2 := store.Service{Name: "SVC-B", Host: "h2", Port: 992}
	populated := map[string]store.Service{"1": svc1, "2": svc2}
	empty := map[string]store.Service{}

	tests := []struct {
		name       string
		key        string
		mapping    map[string]store.Service
		admin      bool
		wantChoice menuChoice
		wantSvc    store.Service
	}{
		// admin "A" with admin=true always wins, even with an empty mapping
		{"admin A, non-empty mapping", "A", populated, true, menuAdmin, store.Service{}},
		{"admin A, empty mapping", "A", empty, true, menuAdmin, store.Service{}},
		// valid service key
		{"valid key 1", "1", populated, false, menuService, svc1},
		{"valid key 2", "2", populated, false, menuService, svc2},
		// invalid key with services present → reprompt in place
		{"invalid key, non-empty mapping", "9", populated, false, menuReprompt, store.Service{}},
		{"non-admin A, non-empty mapping", "A", populated, false, menuReprompt, store.Service{}},
		// any key when mapping is empty → requery (so Session.Run re-queries store)
		{"arbitrary key, empty mapping", "9", empty, false, menuRequery, store.Service{}},
		{"non-admin A, empty mapping", "A", empty, false, menuRequery, store.Service{}},
		{"empty key, empty mapping", "", empty, false, menuRequery, store.Service{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotChoice, gotSvc := classifyMenuSubmit(tc.key, tc.mapping, tc.admin)
			if gotChoice != tc.wantChoice {
				t.Errorf("choice = %v, want %v", gotChoice, tc.wantChoice)
			}
			if gotChoice == menuService && gotSvc != tc.wantSvc {
				t.Errorf("svc = %+v, want %+v", gotSvc, tc.wantSvc)
			}
		})
	}
}

func TestClassifyMenuSubmitUserSettings(t *testing.T) {
	mapping := map[string]store.Service{"1": {Name: "SVC"}}
	if c, _ := classifyMenuSubmit("0", mapping, false); c != menuUserSettings {
		t.Errorf(`classify "0" (non-admin) = %v, want menuUserSettings`, c)
	}
	if c, _ := classifyMenuSubmit("0", mapping, true); c != menuUserSettings {
		t.Errorf(`classify "0" (admin) = %v, want menuUserSettings`, c)
	}
	if c, _ := classifyMenuSubmit("0", map[string]store.Service{}, false); c != menuUserSettings {
		t.Errorf(`classify "0" (empty mapping) = %v, want menuUserSettings`, c)
	}
}

func TestBackendTLSConfig(t *testing.T) {
	if cfg := backendTLSConfig("h:23", BackendTLS{Enabled: false, Verify: true}); cfg != nil {
		t.Errorf("disabled → want nil config, got %+v", cfg)
	}

	on := backendTLSConfig("cics.corp:992", BackendTLS{Enabled: true, Verify: true})
	if on == nil {
		t.Fatal("enabled → want non-nil config")
	}
	if on.InsecureSkipVerify {
		t.Errorf("verify on → InsecureSkipVerify must be false")
	}
	if on.ServerName != "cics.corp" {
		t.Errorf("ServerName = %q, want host %q", on.ServerName, "cics.corp")
	}
	if on.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want TLS 1.2 (%d)", on.MinVersion, tls.VersionTLS12)
	}

	off := backendTLSConfig("10.0.0.5:992", BackendTLS{Enabled: true, Verify: false})
	if off == nil || !off.InsecureSkipVerify {
		t.Errorf("verify off → InsecureSkipVerify must be true, got %+v", off)
	}
	if off.ServerName != "10.0.0.5" {
		t.Errorf("ServerName = %q, want host %q", off.ServerName, "10.0.0.5")
	}
}
