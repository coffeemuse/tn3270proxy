---
name: s3270-smoke-testing
description: Use when verifying protocol-facing behavior of the TN3270 proxy (screens, cursor position, login/menu/bridge flow, PA3/PF3, field attributes) without a human at an emulator — e.g. before declaring 3270 UI work done, or to pre-validate the manual smoke checklist.
---

# Smoke-testing with s3270

## Overview

`s3270` (Homebrew) is the scriptable member of the x3270 family — a real TN3270 emulator driven by actions on stdin, emitting screen dumps and status on stdout. It machine-checks most of the "needs a real emulator" surface: screen content, **cursor position**, **non-display attributes**, PA3/PF3 handling, bridging.

## Quick start

```bash
.claude/skills/s3270-smoke-testing/smoke.sh   # run from repo root
```

Runs the full Task 15 checklist against two throwaway proxy instances; prints PASS/FAIL per item, exits non-zero on failure, leaves evidence in a `/tmp/s3270smoke.*` dir. For new/changed screens, write a focused script with the pattern below.

## Driving s3270

```bash
s3270 -model 3279-2 <<'EOF' > /tmp/out.txt 2>&1
Connect(127.0.0.1:3411)
Wait(5,InputField)
String(alice)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
Ascii()
Quit()
EOF
grep -q "TN3270 GATEWAY MENU" /tmp/out.txt
```

Every action echoes a status line + `ok`/`error`. **Always `Wait(...)` after Connect/Enter/PF/PA before reading** — the screen update is asynchronous. `Wait` also absorbs go3270's NegotiateTelnet read-drain window.

## Quick reference

| Action | Use |
|---|---|
| `Connect(host:port)` | plain; `L:host:port` for TLS (`-noverifycert` for self-signed) |
| `Wait(5,InputField)` | screen arrived + keyboard unlocked + cursor in a field |
| `Wait(5,Output)` | any new output (use before InputField when bridging) |
| `Wait(5,Disconnect)` | host closed — `error` line on timeout means it didn't |
| `String(text)` / `Tab()` | type at cursor / next field. Cursor already lands on the primary field — no `MoveCursor` needed (if you need it, the layout is broken) |
| `Enter()` `PF(3)` `PA(3)` | AID keys |
| `Ascii()` | dump screen as `data:` lines — the assertion workhorse |
| `ReadBuffer(Ascii)` | dump with field attributes: `SF(c0=XX)` |
| `Quit()` | end session |

**Status line** (after every `ok`): `U F U C(127.0.0.1) I 2 24 80 22 16 0x0 0.000` → keyboard, formatted, _, connection (`N` = dropped), mode, model, rows, cols, **cursor row, cursor col (0-based)**, window, time.

- **Cursor check:** login screen must show cursor `22 16` (branding-forward layout: `BodyBottomRow()=22`, userid attr col=15, input col=16 — the go3270 `field.Col+1` gotcha is machine-checkable here).
- **Non-display check:** password field is `SF(c0=cd,...)` (`c0 & 0x0C == 0x0C` = non-display); userid is `c0=c1`.

## Project gotchas

- **The repo's `tn3270proxy.json` is auto-loaded** and binds a TLS listener on :2324 — colliding with any already-running proxy. `-listen` does NOT disable it. Always pass `-config` with `{"listeners":{"plain":{"enabled":true,"addr":"127.0.0.1:PORT"},"tls":{"enabled":false}}}`.
- **Backend for bridge/PA3 tests:** run a *second proxy instance* on another port — a real TN3270 server, fully offline. Bridged sessions land on its login screen; confirm via `accepted connection` in its log.
- **PF3 chain:** menu → login screen (logoff), login → disconnect. Two PF3s to disconnect from the menu; the old checklist wording "PF3 at menu disconnects" is imprecise.
- **Menu order is `ORDER BY name`** (binary: uppercase before lowercase), not seed order — derive selection numbers from the sorted list.
- Use `/tmp` for dbs/configs/seeds; kill only the servers you started (a dev instance often holds :2323/:2324).

## What still needs a human

Colors/intensity rendering, MOD 3/4/5 geometry feel, real-keyboard timing, and emulator-specific quirks (c3270 vs others). s3270 validates content, attributes, cursor, and flow — not visual polish.
