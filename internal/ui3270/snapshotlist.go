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

package ui3270

import "context"

// SnapshotEntry pairs a row's display segments with its domain payload. The
// driver never inspects Item; it hands it to OnSelect.
type SnapshotEntry[T any] struct {
	Row  SnapshotRow
	Item T
}

// SnapshotConfig parameterizes RunSnapshotList. Fetch runs once on entry and
// again only on an explicit refresh (plain Enter), returning the rows, a
// caller-formatted as-of stamp (overflow marker already baked in), and an error
// message (non-"" replaces the rows with the error line). OnSelect handles the
// 'S' line command (nil ⇒ 'S' is inert).
type SnapshotConfig[T any] struct {
	Title, Legend, PFHelp, Empty string
	Head                         SnapshotRow
	Rows                         int // terminal row count → page-size math
	Fetch                        func(ctx context.Context) (rows []SnapshotEntry[T], asOf, errMsg string)
	OnSelect                     func(ctx context.Context, r Renderer, item T) error
}

// RunSnapshotList drives a read-only paged viewer until PF3. It fetches a
// snapshot once and pages the held slice; PF7/PF8 page, plain Enter re-fetches
// (and returns to the newest page), 'S' opens a detail via OnSelect and resumes
// the same snapshot. A non-nil error is a dead connection.
func RunSnapshotList[T any](ctx context.Context, r Renderer, cfg SnapshotConfig[T]) error {
	var (
		rows   []SnapshotEntry[T]
		asOf   string
		errMsg string
		page   int
		loaded bool
	)
	for {
		if !loaded {
			rows, asOf, errMsg = cfg.Fetch(ctx)
			if errMsg != "" {
				rows = nil
			}
			loaded = true
		}
		page, start, end, rowInfo := pageBounds(page, len(rows), cfg.Rows)
		pageRows := rows[start:end]
		display := make([]SnapshotRow, len(pageRows))
		for i, e := range pageRows {
			display[i] = e.Row
		}
		act, err := r.Snapshot(SnapshotView{
			Title: cfg.Title, RowInfo: rowInfo, AsOf: asOf, Head: cfg.Head,
			Rows: display, Legend: cfg.Legend, ErrMsg: errMsg, PFHelp: cfg.PFHelp, Empty: cfg.Empty,
		})
		if err != nil {
			return err
		}
		errMsg = ""

		switch {
		case act.PF == 3:
			return nil
		case act.PF == 7:
			page--
		case act.PF == 8:
			if end < len(rows) {
				page++
			}
		case act.Cmd == 'S':
			if cfg.OnSelect != nil && act.Row < len(pageRows) {
				if ferr := cfg.OnSelect(ctx, r, pageRows[act.Row].Item); ferr != nil {
					return ferr
				}
			}
		case act.Cmd == 0 && act.PF == 0: // plain Enter = refresh
			loaded = false
			page = 0
		}
	}
}
