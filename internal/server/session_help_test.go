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
	"context"
	"testing"

	"github.com/coffeemuse/tn3270proxy/internal/store"
)

// helpMenuLines is the session-side PF1 fetcher: a fresh DB read per call (the
// fresh-on-use convention), nil/empty when the document is blanked.
func TestHelpMenuLines(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := &Session{Store: st}

	// Out of the box: the stock seed renders (non-empty).
	lines, err := s.helpMenuLines(ctx)
	if err != nil || len(lines) == 0 {
		t.Fatalf("stock seed: lines=%d err=%v, want non-empty", len(lines), err)
	}

	// Fresh read: an admin edit shows up on the very next PF1.
	if err := st.SetDocument(ctx, store.DocHelpMenu, "ONE\nTWO", "TEST"); err != nil {
		t.Fatal(err)
	}
	lines, err = s.helpMenuLines(ctx)
	if err != nil || len(lines) != 2 || lines[0] != "ONE" || lines[1] != "TWO" {
		t.Errorf("after edit: got %v (err %v), want [ONE TWO]", lines, err)
	}

	// Blanked: empty result → the presenter shows NO HELP AVAILABLE inline.
	if err := st.SetDocument(ctx, store.DocHelpMenu, "", "TEST"); err != nil {
		t.Fatal(err)
	}
	lines, err = s.helpMenuLines(ctx)
	if err != nil || len(lines) != 0 {
		t.Errorf("blanked: got %v (err %v), want empty", lines, err)
	}
}
