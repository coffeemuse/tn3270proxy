package quickstart

import (
	"regexp"
	"strings"
	"testing"
)

func TestGenPasswordFormat(t *testing.T) {
	re := regexp.MustCompile(`^[ABCDEFGHJKMNPQRSTVWXYZ23456789]{4}-[ABCDEFGHJKMNPQRSTVWXYZ23456789]{4}-[ABCDEFGHJKMNPQRSTVWXYZ23456789]{4}$`)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		p, err := GenPassword()
		if err != nil {
			t.Fatalf("GenPassword: %v", err)
		}
		if !re.MatchString(p) {
			t.Fatalf("password %q does not match XXXX-XXXX-XXXX 3270-safe charset", p)
		}
		if strings.ContainsAny(p, "ILO01") {
			t.Fatalf("password %q contains an ambiguous character", p)
		}
		seen[p] = true
	}
	if len(seen) < 90 {
		t.Fatalf("expected high uniqueness, got %d distinct of 100", len(seen))
	}
}
