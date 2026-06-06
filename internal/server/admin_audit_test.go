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
	"testing"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/racingmars/go3270"
)

func TestAuditEventColor(t *testing.T) {
	red := []string{store.AuditAuthFail, store.AuditAuthError, store.AuditMFAFailed}
	yellow := []string{store.AuditAdmin, store.AuditMFACleared, store.AuditMFAEnforced, store.AuditMFAEnrolled}
	plain := []string{store.AuditConnect, store.AuditAuthOK, store.AuditMFASuccess, store.AuditBridgeStart, store.AuditBridgeEnd, store.AuditLogout, store.AuditDisconnect}

	for _, k := range red {
		if c := auditEventColor(k); c != go3270.Red {
			t.Errorf("color(%s) = %v, want Red", k, c)
		}
	}
	for _, k := range yellow {
		if c := auditEventColor(k); c != go3270.Yellow {
			t.Errorf("color(%s) = %v, want Yellow", k, c)
		}
	}
	for _, k := range plain {
		if c := auditEventColor(k); c != go3270.DefaultColor {
			t.Errorf("color(%s) = %v, want Default", k, c)
		}
	}
}

func TestJulianStamp(t *testing.T) {
	// 2026-06-06 is day-of-year 157 (2026 is not a leap year).
	ts := time.Date(2026, 6, 6, 14, 32, 0, 0, time.UTC)
	if got := julianStamp(ts); got != "2026-06-06 (2026.157)" {
		t.Errorf("julianStamp = %q, want 2026-06-06 (2026.157)", got)
	}
}
