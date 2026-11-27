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

package seed

import (
	"strings"
	"testing"
)

func TestParseDecodesAllKeys(t *testing.T) {
	data, err := Parse([]byte(`{
		"groups": ["ops"],
		"users": [{"username": "alice", "password": "pw", "groups": ["ops"], "user_settings_locked": true}],
		"services": [{"name": "PROD", "description": "Production", "host": "h", "port": 992, "tls": true, "verify": false, "groups": ["ops"]}],
		"trusted_networks": [{"cidr": "127.0.0.1", "comment": "local"}]
	}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(data.Groups) != 1 || len(data.Users) != 1 || len(data.Services) != 1 || len(data.TrustedNetworks) != 1 {
		t.Fatalf("Parse = %+v, want one of each", data)
	}
	if !data.Users[0].UserSettingsLocked {
		t.Error("user_settings_locked should decode")
	}
	if v := data.Services[0].Verify; v == nil || *v {
		t.Errorf("verify should decode to false, got %v", v)
	}
	if n := data.TrustedNetworks[0]; n.CIDR != "127.0.0.1" || n.Comment != "local" {
		t.Errorf("trusted network = %+v", n)
	}
}

func TestParseRejectsUnknownKeys(t *testing.T) {
	cases := map[string]struct {
		raw      string
		misspelt string
	}{
		// A misspelt top-level list would otherwise skip every entry in it.
		"top level": {`{"trusted_network": [{"cidr": "127.0.0.1", "comment": "local"}]}`, "trusted_network"},
		// A misspelt field inside an entry would otherwise apply the default —
		// here, leaving a shared guest account unlocked.
		"user entry":    {`{"users": [{"username": "guest", "password": "pw", "user_settings_lock": true}]}`, "user_settings_lock"},
		"service entry": {`{"services": [{"name": "LAB", "description": "Lab", "host": "h", "port": 992, "verfiy": false}]}`, "verfiy"},
		"network entry": {`{"trusted_networks": [{"cidr": "127.0.0.1", "comments": "local"}]}`, "comments"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(tc.raw))
			if err == nil {
				t.Fatal("Parse should reject an unknown key, got nil")
			}
			if !strings.Contains(err.Error(), tc.misspelt) {
				t.Errorf("error should name the unknown key %q, got: %v", tc.misspelt, err)
			}
		})
	}
}

func TestParseRejectsTrailingData(t *testing.T) {
	// json.Unmarshal (the previous decoder) rejected this; the strict decoder
	// must not quietly apply only the first object.
	if _, err := Parse([]byte(`{"groups": ["ops"]} {"groups": ["dev"]}`)); err == nil {
		t.Fatal("Parse should reject data after the top-level object, got nil")
	}
}

func TestParseRejectsMalformedJSON(t *testing.T) {
	if _, err := Parse([]byte(`{"groups": [`)); err == nil {
		t.Fatal("Parse should reject malformed JSON, got nil")
	}
}
