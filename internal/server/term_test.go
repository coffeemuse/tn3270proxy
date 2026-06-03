package server

import "testing"

func TestNormalizeTermFallsBackTo24x80(t *testing.T) {
	cases := []struct {
		name               string
		in                 Term
		wantRows, wantCols int
	}{
		{"zero dims", Term{Type: "IBM-3278-2"}, 24, 80},
		{"sub-MOD 2", Term{Type: "IBM-DYNAMIC", Rows: 12, Cols: 40}, 24, 80},
		{"narrow", Term{Type: "IBM-DYNAMIC", Rows: 43, Cols: 40}, 24, 80},
		{"MOD 2 exact", Term{Type: "IBM-3278-2", Rows: 24, Cols: 80}, 24, 80},
		{"MOD 4 kept", Term{Type: "IBM-3278-4", Rows: 43, Cols: 80}, 43, 80},
		{"MOD 5 kept", Term{Type: "IBM-3278-5", Rows: 27, Cols: 132}, 27, 132},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := normalizeTerm(c.in)
			if got.Rows != c.wantRows || got.Cols != c.wantCols {
				t.Errorf("normalizeTerm(%+v) = %dx%d, want %dx%d", c.in, got.Rows, got.Cols, c.wantRows, c.wantCols)
			}
			if got.Type != c.in.Type {
				t.Errorf("Type changed: %q → %q", c.in.Type, got.Type)
			}
			// dev is always nil in tests (go3270.DevInfo cannot be faked);
			// this guards against future refactors that might populate dev.
			if (c.wantRows == 24 && c.in.Rows != 24) && got.dev != nil {
				t.Errorf("fallback must drop dev")
			}
		})
	}
}

func TestTermGeometryAndCodepage(t *testing.T) {
	tm := Term{Type: "IBM-3278-4", Rows: 43, Cols: 80}
	if g := tm.Geometry(); g.Rows != 43 || g.Cols != 80 {
		t.Errorf("Geometry() = %+v", g)
	}
	if cp := tm.codepage(); cp != nil {
		t.Errorf("nil dev → codepage must be nil, got %v", cp)
	}
}
