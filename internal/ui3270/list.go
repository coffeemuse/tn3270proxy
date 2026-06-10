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

// RunList drives a paginated line-command list until PF3. It owns paging, the
// delete-confirm dance, and command dispatch; the caller supplies data (Fetch)
// and behavior (Cmds/Add). A non-nil error is a dead connection.
func RunList[T any](ctx context.Context, r Renderer, cfg ListConfig[T]) error {
	page, errMsg := 0, ""
	var pending *pendingConfirm[T]
	for {
		rows, ferr := cfg.Fetch(ctx)
		if ferr != "" {
			errMsg = ferr
			rows = nil
			pending = nil // confirm lost; user must re-initiate
		}
		var start, end int
		var rowInfo string
		page, start, end, rowInfo = pageBounds(page, len(rows), cfg.Rows)
		pageRows := rows[start:end]
		display := make([]string, len(pageRows))
		for i, row := range pageRows {
			display[i] = row.Display
		}
		act, err := r.List(ListView{
			Title: cfg.Title, RowInfo: rowInfo, Header: cfg.Header,
			Rows: display, Legend: cfg.Legend, ErrMsg: errMsg, PFHelp: cfg.PFHelp,
		})
		if err != nil {
			return err
		}
		errMsg = ""

		// A pending confirm is resolved by the very next action: plain Enter
		// commits, PF3 cancels, anything else cancels and is processed normally.
		if pending != nil {
			target := pending.item
			cmd := pending.cmd
			pending = nil
			switch {
			case act.Cmd == 0 && act.PF == 0:
				msg, ferr := cmd.Commit(ctx, r, target)
				if ferr != nil {
					return ferr
				}
				errMsg = msg
				continue
			case act.PF == 3:
				continue
				// Any other action: pending is already nil above, so the confirm
				// is cancelled and control deliberately falls through to the main
				// switch below to process the action normally. Do NOT add a
				// "default: continue" here — that would silently swallow it.
			}
		}

		switch {
		case act.PF == 3:
			return nil
		case act.PF == 4:
			if cfg.Add != nil {
				msg, ferr := cfg.Add(ctx, r)
				if ferr != nil {
					return ferr
				}
				errMsg = msg
			}
		case act.PF == 7:
			page--
		case act.PF == 8:
			if end < len(rows) {
				page++
			}
		case act.Cmd != 0:
			if act.Row >= len(pageRows) {
				continue
			}
			item := pageRows[act.Row].Item
			cmd := findCmd(cfg.Cmds, act.Cmd)
			if cmd == nil {
				errMsg = "INVALID COMMAND: " + string(act.Cmd)
				continue
			}
			if cmd.Confirm == nil {
				msg, ferr := cmd.Commit(ctx, r, item)
				if ferr != nil {
					return ferr
				}
				errMsg = msg
				continue
			}
			prompt, blocked := cmd.Confirm(item)
			if blocked != "" {
				errMsg = blocked
				continue
			}
			pending = &pendingConfirm[T]{cmd: *cmd, item: item}
			errMsg = prompt
		}
	}
}

type pendingConfirm[T any] struct {
	cmd  Command[T]
	item T
}

func findCmd[T any](cmds []Command[T], key byte) *Command[T] {
	for i := range cmds {
		if cmds[i].Key == key {
			return &cmds[i]
		}
	}
	return nil
}
