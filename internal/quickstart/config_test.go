package quickstart

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderConfigJSON(t *testing.T) {
	l := NewLayout("/data")
	b, err := renderConfigJSON(l)
	if err != nil {
		t.Fatalf("renderConfigJSON: %v", err)
	}
	// Must unmarshal with unknown-field rejection (mirrors internal/config loader).
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	var got map[string]any
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("generated config has fields the loader would reject: %v", err)
	}
	if !strings.Contains(string(b), "\"db\": \"/data/proxy.db\"") &&
		!strings.Contains(string(b), "/data/proxy.db") {
		t.Errorf("db path missing from config:\n%s", b)
	}
	if !strings.Contains(string(b), TLSAddr) {
		t.Errorf("tls addr %q missing from config:\n%s", TLSAddr, b)
	}
	if !strings.Contains(string(b), "/data/mfa.key") {
		t.Errorf("mfa key_file missing from config:\n%s", b)
	}
}
