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
