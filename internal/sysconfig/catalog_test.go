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

package sysconfig

import "testing"

func TestCatalogHasMOTDFILE(t *testing.T) {
	var found *Entry
	for i := range Catalog {
		if Catalog[i].Key == "MOTD_FILE" {
			found = &Catalog[i]
			break
		}
	}
	if found == nil {
		t.Fatal("MOTD_FILE not in catalog")
	}
	if found.Label == "" {
		t.Error("MOTD_FILE label is empty")
	}
	if found.Validate == nil {
		t.Error("MOTD_FILE Validate is nil")
	}
	// empty = feature disabled; must be valid
	if msg := found.Validate(""); msg != "" {
		t.Errorf("empty MOTD_FILE: Validate returned %q, want empty", msg)
	}
	// any non-empty path is also accepted
	if msg := found.Validate("/etc/motd.txt"); msg != "" {
		t.Errorf("path MOTD_FILE: Validate returned %q, want empty", msg)
	}
}

func TestCatalogHasBrandingFile(t *testing.T) {
	var e *Entry
	for i := range Catalog {
		if Catalog[i].Key == KeyBrandingFile {
			e = &Catalog[i]
			break
		}
	}
	if e == nil {
		t.Fatalf("Catalog missing %q entry", KeyBrandingFile)
	}
	if e.Default != "" {
		t.Errorf("BRANDING_FILE default = %q, want empty (disabled)", e.Default)
	}
	if e.Validate == nil || e.Validate("/any/path") != "" || e.Validate("") != "" {
		t.Errorf("BRANDING_FILE validator should accept any value")
	}
}

func TestCatalogKeysAreUppercase(t *testing.T) {
	for _, e := range Catalog {
		if e.Key == "" {
			t.Error("catalog entry has empty key")
		}
		for _, r := range e.Key {
			if r >= 'a' && r <= 'z' {
				t.Errorf("key %q contains lowercase character %q", e.Key, r)
				break
			}
		}
	}
}

func TestCatalogLabelsNonEmpty(t *testing.T) {
	for _, e := range Catalog {
		if e.Label == "" {
			t.Errorf("catalog entry %q has empty label", e.Key)
		}
	}
}

func TestKeyMOTDFileConstant(t *testing.T) {
	if KeyMOTDFile != "MOTD_FILE" {
		t.Errorf("KeyMOTDFile = %q, want MOTD_FILE", KeyMOTDFile)
	}
	found := false
	for _, e := range Catalog {
		if e.Key == KeyMOTDFile {
			found = true
		}
	}
	if !found {
		t.Errorf("Catalog has no entry keyed by KeyMOTDFile")
	}
}

func issuerEntry(t *testing.T) Entry {
	t.Helper()
	for _, e := range Catalog {
		if e.Key == KeyMFAIssuer {
			return e
		}
	}
	t.Fatal("MFA_ISSUER not in Catalog")
	return Entry{}
}

func TestMFAIssuerDefault(t *testing.T) {
	if issuerEntry(t).Default != "TN3270PROXY" {
		t.Fatalf("unexpected default: %q", issuerEntry(t).Default)
	}
}

func TestMFAIssuerValidation(t *testing.T) {
	v := issuerEntry(t).Validate
	if v("TN3270PROXY") != "" {
		t.Fatal("valid issuer rejected")
	}
	if v("") == "" {
		t.Fatal("empty issuer should be rejected")
	}
	if v("has:colon") == "" {
		t.Fatal("colon should be rejected")
	}
	if v("THIS-NAME-IS-WAY-TOO-LONG-TO-FIT-IN-FORTY-CHARS!!") == "" {
		t.Fatal("over-length issuer should be rejected")
	}
}

func TestCatalogThrottleParams(t *testing.T) {
	want := map[string]string{
		KeyAuthDelayBaseSecs:  "2",
		KeyAuthMaxTries:       "5",
		KeyAuthFailWindowMins: "15",
	}
	for key, def := range want {
		var found *Entry
		for i := range Catalog {
			if Catalog[i].Key == key {
				found = &Catalog[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("%s not in catalog", key)
		}
		if found.Label == "" {
			t.Errorf("%s label is empty", key)
		}
		if found.Default != def {
			t.Errorf("%s default = %q, want %q", key, found.Default, def)
		}
		if found.Validate == nil {
			t.Fatalf("%s Validate is nil", key)
		}
	}
}

func TestThrottleValidators(t *testing.T) {
	base := entryByKey(t, KeyAuthDelayBaseSecs)
	if msg := base.Validate("0"); msg != "" { // 0 is the off-switch, must be valid
		t.Errorf("base 0: got %q, want valid", msg)
	}
	if msg := base.Validate("-1"); msg == "" {
		t.Error("base -1: want error")
	}
	if msg := base.Validate("x"); msg == "" {
		t.Error("base x: want error")
	}
	tries := entryByKey(t, KeyAuthMaxTries)
	if msg := tries.Validate("0"); msg != "" { // max-tries 0 disables, must be valid
		t.Errorf("max-tries 0: got %q, want valid", msg)
	}
	if msg := tries.Validate("-1"); msg == "" {
		t.Error("max-tries -1: want error")
	}
	win := entryByKey(t, KeyAuthFailWindowMins)
	if msg := win.Validate("0"); msg == "" { // window must be >= 1
		t.Error("window 0: want error")
	}
	if msg := win.Validate("1"); msg != "" {
		t.Errorf("window 1: got %q, want valid", msg)
	}
}

func TestSystemIDEntry(t *testing.T) {
	e := entryByKey(t, KeySystemID)
	if KeySystemID != "SYSTEM_ID" {
		t.Errorf("KeySystemID = %q, want SYSTEM_ID", KeySystemID)
	}
	if e.Default != "PROXY" {
		t.Errorf("SYSTEM_ID default = %q, want PROXY", e.Default)
	}
	if e.Label != "System ID:" {
		t.Errorf("SYSTEM_ID label = %q, want %q", e.Label, "System ID:")
	}
	if e.Length != 7 {
		t.Errorf("SYSTEM_ID length = %d, want 7", e.Length)
	}
	if e.Normalize == nil {
		t.Fatal("SYSTEM_ID Normalize is nil")
	}
	if e.Validate == nil {
		t.Fatal("SYSTEM_ID Validate is nil")
	}
}

func TestSystemIDNormalize(t *testing.T) {
	n := entryByKey(t, KeySystemID).Normalize
	for in, want := range map[string]string{
		" proxy ": "PROXY",
		"sysa":    "SYSA",
		"SYS-01":  "SYS-01",
		"  a1":    "A1",
	} {
		if got := n(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSystemIDValidate(t *testing.T) {
	v := entryByKey(t, KeySystemID).Validate
	for _, ok := range []string{"PROXY", "SYSA", "A1", "SYS-01", "A-B-C-D", "ABCDEFG"} {
		if msg := v(ok); msg != "" {
			t.Errorf("Validate(%q) = %q, want valid", ok, msg)
		}
	}
	for in, want := range map[string]string{
		"":         "SYSTEM ID REQUIRED",
		"ABCDEFGH": "SYSTEM ID TOO LONG (MAX 7)",
		"-SYS":     "SYSTEM ID MUST NOT START WITH A DASH",
		"SYS_01":   "SYSTEM ID: USE A-Z 0-9 AND DASH",
		"SYS.1":    "SYSTEM ID: USE A-Z 0-9 AND DASH",
		"SYS@":     "SYSTEM ID: USE A-Z 0-9 AND DASH",
	} {
		if msg := v(in); msg != want {
			t.Errorf("Validate(%q) = %q, want %q", in, msg, want)
		}
	}
}

func TestAuditCatalogEntries(t *testing.T) {
	want := map[string]string{
		KeyAuditMaxRows:    "1000",
		KeyAuditReverseDNS: "Y",
	}
	got := map[string]string{}
	for _, e := range Catalog {
		if _, ok := want[e.Key]; ok {
			got[e.Key] = e.Default
			if e.Validate == nil {
				t.Errorf("%s has no Validate", e.Key)
			}
		}
	}
	for k, def := range want {
		if got[k] != def {
			t.Errorf("Catalog[%s].Default = %q, want %q", k, got[k], def)
		}
	}
	// AUDIT_MAX_ROWS bounds: 0 and 10001 rejected, 1000 accepted.
	for _, e := range Catalog {
		if e.Key == KeyAuditMaxRows {
			if e.Validate("0") == "" || e.Validate("10001") == "" || e.Validate("1000") != "" {
				t.Errorf("AUDIT_MAX_ROWS validator bounds wrong")
			}
		}
	}
}

func entryByKey(t *testing.T, key string) *Entry {
	t.Helper()
	for i := range Catalog {
		if Catalog[i].Key == key {
			return &Catalog[i]
		}
	}
	t.Fatalf("%s not in catalog", key)
	return nil
}

func TestIntInRange(t *testing.T) {
	v := intInRange(1, 10000)
	for _, ok := range []string{"1", "10000", " 500 "} {
		if msg := v(ok); msg != "" {
			t.Errorf("intInRange(%q) = %q, want accept", ok, msg)
		}
	}
	for _, bad := range []string{"0", "10001", "-1", "", "x", "1.5"} {
		if v(bad) == "" {
			t.Errorf("intInRange(%q) accepted, want reject", bad)
		}
	}
}

func TestYesNo(t *testing.T) {
	for _, ok := range []string{"Y", "N", " y ", "n"} {
		if msg := yesNo(ok); msg != "" {
			t.Errorf("yesNo(%q) = %q, want accept", ok, msg)
		}
	}
	for _, bad := range []string{"", "YES", "ON", "1", "TRUE"} {
		if yesNo(bad) == "" {
			t.Errorf("yesNo(%q) accepted, want reject", bad)
		}
	}
}
