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
