package store

import (
	"context"
	"testing"
	"time"
)

func TestRecordAndListAudit(t *testing.T) {
	st := newTestStore(t) // helper from store_test.go
	ctx := context.Background()
	t0 := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	events := []AuditEvent{
		{At: t0, SessionID: "s1", Kind: AuditConnect, RemoteAddr: "10.0.0.5:40000"},
		{At: t0.Add(time.Second), SessionID: "s1", Kind: AuditAuthOK, Username: "alice", RemoteAddr: "10.0.0.5:40000"},
		{At: t0.Add(2 * time.Second), SessionID: "s1", Kind: AuditDisconnect, RemoteAddr: "10.0.0.5:40000", Detail: "client disconnected"},
	}
	for _, ev := range events {
		if err := st.RecordAudit(ctx, ev); err != nil {
			t.Fatalf("RecordAudit(%s): %v", ev.Kind, err)
		}
	}

	got, err := st.ListAudit(ctx, AuditFilter{})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// Newest first (id DESC — RFC3339 is second-granular, id preserves insert order).
	if got[0].Kind != AuditDisconnect || got[1].Kind != AuditAuthOK || got[2].Kind != AuditConnect {
		t.Errorf("order = %s,%s,%s, want disconnect,auth_ok,connect", got[0].Kind, got[1].Kind, got[2].Kind)
	}
	if !got[2].At.Equal(t0) {
		t.Errorf("At round-trip = %v, want %v", got[2].At, t0)
	}
	if got[1].Username != "alice" || got[0].Detail != "client disconnected" || got[2].RemoteAddr != "10.0.0.5:40000" {
		t.Errorf("field round-trip failed: %+v", got)
	}
	if got[0].SessionID != "s1" || got[0].ID == 0 {
		t.Errorf("SessionID/ID round-trip failed: %+v", got[0])
	}
}

func TestListAuditFilters(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	seed := []AuditEvent{
		{At: t0, SessionID: "s1", Kind: AuditAuthFail, Username: "mallory"},
		{At: t0.Add(time.Minute), SessionID: "s2", Kind: AuditAuthOK, Username: "alice"},
		{At: t0.Add(2 * time.Minute), SessionID: "s2", Kind: AuditBridgeStart, Username: "alice", Service: "PROD"},
		{At: t0.Add(time.Hour), SessionID: "s3", Kind: AuditAuthFail, Username: "mallory"},
	}
	for _, ev := range seed {
		if err := st.RecordAudit(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name      string
		f         AuditFilter
		wantKinds []string
	}{
		{"by username", AuditFilter{Username: "alice"}, []string{AuditBridgeStart, AuditAuthOK}},
		{"by kind", AuditFilter{Kind: AuditAuthFail}, []string{AuditAuthFail, AuditAuthFail}},
		{"since cuts older", AuditFilter{Since: t0.Add(30 * time.Minute)}, []string{AuditAuthFail}},
		{"combined AND", AuditFilter{Username: "mallory", Since: t0.Add(30 * time.Minute)}, []string{AuditAuthFail}},
		{"limit", AuditFilter{Limit: 2}, []string{AuditAuthFail, AuditBridgeStart}},
		{"no match", AuditFilter{Username: "nobody"}, nil},
	}
	for _, c := range cases {
		got, err := st.ListAudit(ctx, c.f)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		kinds := make([]string, 0, len(got))
		for _, ev := range got {
			kinds = append(kinds, ev.Kind)
		}
		if len(kinds) != len(c.wantKinds) {
			t.Errorf("%s: kinds = %v, want %v", c.name, kinds, c.wantKinds)
			continue
		}
		for i := range kinds {
			if kinds[i] != c.wantKinds[i] {
				t.Errorf("%s: kinds = %v, want %v", c.name, kinds, c.wantKinds)
				break
			}
		}
	}
}

func TestPruneAudit(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	for _, ev := range []AuditEvent{
		{At: t0.Add(-100 * 24 * time.Hour), SessionID: "old", Kind: AuditConnect},
		{At: t0.Add(-91 * 24 * time.Hour), SessionID: "old2", Kind: AuditDisconnect},
		{At: t0, SessionID: "new", Kind: AuditConnect},
	} {
		if err := st.RecordAudit(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	n, err := st.PruneAudit(ctx, t0.Add(-90*24*time.Hour))
	if err != nil {
		t.Fatalf("PruneAudit: %v", err)
	}
	if n != 2 {
		t.Errorf("pruned = %d, want 2", n)
	}
	got, err := st.ListAudit(ctx, AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SessionID != "new" {
		t.Errorf("remaining = %+v, want only session 'new'", got)
	}

	// Pruning again is a no-op.
	n, err = st.PruneAudit(ctx, t0.Add(-90*24*time.Hour))
	if err != nil || n != 0 {
		t.Errorf("second prune = (%d, %v), want (0, nil)", n, err)
	}
}
