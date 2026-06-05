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
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
)

// Auditor records audit events. Recording is best-effort by contract: Record
// returns no error and implementations must never block or fail the session.
type Auditor interface {
	Record(ctx context.Context, ev store.AuditEvent)
}

// storeAuditor writes audit events to the store, stamping the time. Failures
// are logged and swallowed (best-effort — a DB hiccup must not kick users off).
type storeAuditor struct {
	store  *store.Store
	logger *slog.Logger // nil → slog.Default()
}

func (a storeAuditor) log() *slog.Logger {
	if a.logger != nil {
		return a.logger
	}
	return slog.Default()
}

func (a storeAuditor) Record(ctx context.Context, ev store.AuditEvent) {
	ev.At = time.Now().UTC()
	if err := a.store.RecordAudit(ctx, ev); err != nil {
		a.log().Error("audit record failed", "event", ev.Kind, "error", err)
	}
}

// auditTrail prefills one connection's identity (session id + remote addr)
// into every event, so call sites only supply Kind/Username/Service/Detail.
type auditTrail struct {
	auditor    Auditor
	sessionID  string
	remoteAddr string
}

// newAuditTrail builds the connection's trail. A nil Session.Auditor yields a
// trail whose record() is a no-op (mirrors the nil-AdminPresenter pattern).
func (s *Session) newAuditTrail(conn net.Conn) *auditTrail {
	addr := ""
	if ra := conn.RemoteAddr(); ra != nil {
		addr = ra.String()
	}
	return &auditTrail{auditor: s.Auditor, sessionID: newSessionID(), remoteAddr: addr}
}

func (a *auditTrail) record(ctx context.Context, ev store.AuditEvent) {
	if a.auditor == nil {
		return
	}
	ev.SessionID = a.sessionID
	ev.RemoteAddr = a.remoteAddr
	a.auditor.Record(ctx, ev)
}

// newSessionID returns 8 random bytes hex-encoded (16 chars).
func newSessionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "unknown" // crypto/rand does not fail in practice
	}
	return hex.EncodeToString(b)
}
