package server

import (
	"context"
	"testing"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

func TestStoreAuditorRecordsAndStampsTime(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := storeAuditor{store: st}

	a.Record(context.Background(), store.AuditEvent{
		Kind: store.AuditConnect, SessionID: "abcd", RemoteAddr: "10.0.0.5:40000"})

	evs, err := st.ListAudit(context.Background(), store.AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Kind != store.AuditConnect || evs[0].At.IsZero() {
		t.Errorf("events = %+v, want one connect with a stamped time", evs)
	}
}
