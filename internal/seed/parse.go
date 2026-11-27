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
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Parse strictly decodes a JSON seed file. Unknown keys are rejected at every
// level: a misspelt key would otherwise be silently dropped, skipping a whole
// list or applying a default (e.g. leaving a guest account unlocked) — and
// seed is a one-time tool, so the mistake can't simply be re-run away. It
// also means a key added in a later release fails loudly here instead of
// being ignored by a binary that predates it.
func Parse(raw []byte) (SeedData, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var data SeedData
	if err := dec.Decode(&data); err != nil {
		return SeedData{}, err
	}
	// Decode stops after the first value; reject anything after it, as
	// json.Unmarshal would.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return SeedData{}, errors.New("unexpected data after the top-level JSON object")
	}
	return data, nil
}
