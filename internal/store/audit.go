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

package store

import (
	"context"
	"strings"
	"time"
)

// Audit event kinds. One session_id ties a connection's events together.
const (
	AuditConnect           = "connect"            // TCP session began
	AuditAuthOK            = "auth_ok"            // login success
	AuditAuthFail          = "auth_fail"          // login failure (attempted username, never the password)
	AuditAuthError         = "auth_error"         // infrastructure error during authentication (detail = error text)
	AuditBridgeStart       = "bridge_start"       // service selected, backend dial begins
	AuditBridgeEnd         = "bridge_end"         // bridge returned (detail = cause)
	AuditAdmin             = "admin"              // admin CRUD mutation (detail = change description)
	AuditLogout            = "logout"             // session ended login (detail: "user logoff" | "idle logout")
	AuditDisconnect        = "disconnect"         // connection ended (detail = how)
	AuditMFAEnrolled       = "mfa_enrolled"       // user completed self-enrollment
	AuditMFASuccess        = "mfa_success"        // correct code at login
	AuditMFAFailed         = "mfa_failed"         // incorrect code (enroll confirm or login)
	AuditMFACleared        = "mfa_cleared"        // admin wiped the secret
	AuditMFAEnforced       = "mfa_enforced"       // admin turned mfa_required on
	AuditPasswordSelf      = "password_self"      // user changed their own password (self-service)
	AuditSettingsLocked    = "settings_locked"    // admin locked a user out of self-service
	AuditSettingsUnlocked  = "settings_unlocked"  // admin restored a user's self-service
	AuditSessionDisconnect = "session_disconnect" // admin disconnected a live session (GH #91)
	AuditDocUpdate         = "doc_update"         // admin saved a document from the editor (detail = name + line count)
	AuditDocImport         = "doc_import"         // document replaced from a server file (detail = name + path + line count)
)

// AuditEvent is one audit-trail row. Username is a plain string, not a user
// FK: rows must survive user deletion, and auth_fail records usernames that
// may not exist.
type AuditEvent struct {
	ID         int64
	At         time.Time
	SessionID  string
	Kind       string
	Username   string // the subject — the account this row is about ("" if none)
	Actor      string // the authenticated principal who performed the action ("" pre-auth)
	RemoteAddr string
	Service    string
	Detail     string
}

// RecordAudit inserts ev. At is stored as UTC RFC3339 (second resolution;
// the autoincrement id preserves insert order within a second).
func (s *Store) RecordAudit(ctx context.Context, ev AuditEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit (at, session_id, kind, username, actor, remote_addr, service, detail)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.At.UTC().Format(time.RFC3339), ev.SessionID, ev.Kind,
		ev.Username, ev.Actor, ev.RemoteAddr, ev.Service, ev.Detail)
	return err
}

// AuditFilter narrows ListAudit. Zero values mean "no constraint";
// Limit <= 0 means the default of 100 rows.
type AuditFilter struct {
	Username string // subject lens
	Actor    string // actor lens
	Kind     string
	Since    time.Time
	Limit    int
}

// ListAudit returns matching events, newest first. Filters AND-combine.
func (s *Store) ListAudit(ctx context.Context, f AuditFilter) ([]AuditEvent, error) {
	var where []string
	var args []any
	if f.Username != "" {
		where = append(where, "username = ?")
		args = append(args, f.Username)
	}
	if f.Actor != "" {
		where = append(where, "actor = ?")
		args = append(args, f.Actor)
	}
	if f.Kind != "" {
		where = append(where, "kind = ?")
		args = append(args, f.Kind)
	}
	if !f.Since.IsZero() {
		where = append(where, "at >= ?")
		args = append(args, f.Since.UTC().Format(time.RFC3339))
	}
	query := `SELECT id, at, session_id, kind, username, actor, remote_addr, service, detail FROM audit`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var ev AuditEvent
		var at string
		if err := rows.Scan(&ev.ID, &at, &ev.SessionID, &ev.Kind,
			&ev.Username, &ev.Actor, &ev.RemoteAddr, &ev.Service, &ev.Detail); err != nil {
			return nil, err
		}
		if ev.At, err = time.Parse(time.RFC3339, at); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// PruneAudit deletes events strictly older than before; returns rows deleted.
func (s *Store) PruneAudit(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM audit WHERE at < ?", before.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
