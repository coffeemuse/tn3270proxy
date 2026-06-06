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
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/CoffeeMuse/tn3270proxy/internal/store"
	"github.com/CoffeeMuse/tn3270proxy/internal/sysconfig"
	"github.com/racingmars/go3270"
)

// auditEventColor maps an audit kind to its display colour (derived severity;
// there is no stored severity). Only failures (red) and security-state changes
// (yellow) are coloured; routine events stay plain so the exceptions pop.
func auditEventColor(kind string) go3270.Color {
	switch kind {
	case store.AuditAuthFail, store.AuditAuthError, store.AuditMFAFailed:
		return go3270.Red
	case store.AuditAdmin, store.AuditMFACleared, store.AuditMFAEnforced, store.AuditMFAEnrolled:
		return go3270.Yellow
	default:
		return go3270.DefaultColor
	}
}

// julianStamp formats t as "YYYY-MM-DD (YYYY.DDD)" with the day-of-year, used on
// both the list as-of header and the detail Date/Time line.
func julianStamp(t time.Time) string {
	return fmt.Sprintf("%s (%d.%03d)", t.Format("2006-01-02"), t.Year(), t.YearDay())
}

// auditCap reads AUDIT_MAX_ROWS, clamped to [1,10000] with a fallback to the
// default for a hand-edited DB (mirrors Session.throttleInt).
func (f *adminFlow) auditCap(ctx context.Context) int {
	v, err := f.store.GetConfig(ctx, sysconfig.KeyAuditMaxRows)
	if err != nil {
		return sysconfig.DefaultAuditMaxRows
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 || n > 10000 {
		return sysconfig.DefaultAuditMaxRows
	}
	return n
}

// auditReverseDNS reports whether the reverse-DNS lookup is enabled
// (AUDIT_REVERSE_DNS == "ON"; default on when unreadable).
func (f *adminFlow) auditReverseDNS(ctx context.Context) bool {
	v, err := f.store.GetConfig(ctx, sysconfig.KeyAuditReverseDNS)
	if err != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(v), "ON")
}
