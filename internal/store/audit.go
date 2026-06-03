package store

import (
	"context"
	"time"
)

// Audit event kinds. One session_id ties a connection's events together.
const (
	AuditConnect     = "connect"      // TCP session began
	AuditAuthOK      = "auth_ok"      // login success
	AuditAuthFail    = "auth_fail"    // login failure (attempted username, never the password)
	AuditBridgeStart = "bridge_start" // service selected, backend dial begins
	AuditBridgeEnd   = "bridge_end"   // bridge returned (detail = cause)
	AuditAdmin       = "admin"        // admin CRUD mutation (detail = change description)
	AuditDisconnect  = "disconnect"   // connection ended (detail = how)
)

// AuditEvent is one audit-trail row. Username is a plain string, not a user
// FK: rows must survive user deletion, and auth_fail records usernames that
// may not exist.
type AuditEvent struct {
	ID         int64
	At         time.Time
	SessionID  string
	Kind       string
	Username   string
	RemoteAddr string
	Service    string
	Detail     string
}

// RecordAudit inserts ev. At is stored as UTC RFC3339 (second resolution;
// the autoincrement id preserves insert order within a second).
func (s *Store) RecordAudit(ctx context.Context, ev AuditEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit (at, session_id, kind, username, remote_addr, service, detail)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ev.At.UTC().Format(time.RFC3339), ev.SessionID, ev.Kind,
		ev.Username, ev.RemoteAddr, ev.Service, ev.Detail)
	return err
}

// AuditFilter narrows ListAudit. Zero values mean "no constraint";
// Limit <= 0 means the default of 100 rows.
type AuditFilter struct {
	Username string
	Kind     string
	Since    time.Time
	Limit    int
}

// ListAudit returns matching events, newest first.
func (s *Store) ListAudit(ctx context.Context, f AuditFilter) ([]AuditEvent, error) {
	query := `SELECT id, at, session_id, kind, username, remote_addr, service, detail
		FROM audit ORDER BY id DESC LIMIT ?`
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var ev AuditEvent
		var at string
		if err := rows.Scan(&ev.ID, &at, &ev.SessionID, &ev.Kind,
			&ev.Username, &ev.RemoteAddr, &ev.Service, &ev.Detail); err != nil {
			return nil, err
		}
		if ev.At, err = time.Parse(time.RFC3339, at); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
