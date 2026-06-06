#!/usr/bin/env bash
# Automated TN3270 smoke test for tn3270proxy, driven by s3270.
# Run from the repo root:  .claude/skills/s3270-smoke-testing/smoke.sh
# Covers the Task 15 manual checklist (login render, non-display password,
# bad password, group-filtered menu, bridge, PA3, unreachable service, PF3).
# Override ports with FRONT_PORT / BACK_PORT env vars if the defaults are taken.
set -u

FRONT_PORT="${FRONT_PORT:-3411}"
BACK_PORT="${BACK_PORT:-3412}"
IDLE_PORT="${IDLE_PORT:-3413}"
WORK="$(mktemp -d /tmp/s3270smoke.XXXXXX)"
PASS=0 FAIL=0

cleanup() {
  [ -n "${FRONT_PID:-}" ] && kill "$FRONT_PID" 2>/dev/null
  [ -n "${BACK_PID:-}" ] && kill "$BACK_PID" 2>/dev/null
  [ -n "${IDLE_PID:-}" ] && kill "$IDLE_PID" 2>/dev/null
}
trap cleanup EXIT

check() { # check <name> <grep-BRE-pattern> <file>
  if grep -q "$2" "$3"; then PASS=$((PASS+1)); echo "PASS: $1"
  else FAIL=$((FAIL+1)); echo "FAIL: $1 (pattern '$2' not found in $3)"; fi
}
ncheck() { # ncheck <name> <pattern-that-must-NOT-appear> <file>
  if ! grep -q "$2" "$3"; then PASS=$((PASS+1)); echo "PASS: $1"
  else FAIL=$((FAIL+1)); echo "FAIL: $1 (pattern '$2' unexpectedly found in $3)"; fi
}
s3() { s3270 -model 3279-2 > "$WORK/$1.out" 2>&1; }

# --- build ---
go build -o "$WORK/tn3270proxy" ./cmd/tn3270proxy || { echo "FAIL: build"; exit 1; }

# --- configs: each instance gets an explicit -config that DISABLES the TLS
# listener. Without it the repo's tn3270proxy.json is auto-loaded and tries to
# bind :2324, colliding with any already-running proxy. -listen alone does NOT
# disable the file's TLS listener. ---
cat > "$WORK/front-cfg.json" <<EOF
{"listeners":{"plain":{"enabled":true,"addr":"127.0.0.1:$FRONT_PORT"},"tls":{"enabled":false}}}
EOF
cat > "$WORK/back-cfg.json" <<EOF
{"listeners":{"plain":{"enabled":true,"addr":"127.0.0.1:$BACK_PORT"},"tls":{"enabled":false}}}
EOF

# Front seed: alice(ops) sees BACKEND (the second proxy) + DEADHOST (port 1,
# nothing listens). DEVONLY(dev) must NOT appear on her menu. charlie(empty) is
# in a group with no services — a legitimately empty menu, used by scenario 8.
cat > "$WORK/front-seed.json" <<EOF
{"groups":["ops","dev","empty"],
 "users":[
  {"username":"alice","password":"changeme","groups":["ops"]},
  {"username":"charlie","password":"changeme","groups":["empty"]},
  {"username":"admin","password":"changeme","groups":["ZZADMIN"]}],
 "services":[
  {"name":"BACKEND","description":"Backend Host","host":"127.0.0.1","port":$BACK_PORT,"groups":["ops"]},
  {"name":"DEADHOST","description":"Dead Host","host":"127.0.0.1","port":1,"groups":["ops"]},
  {"name":"WIDEDESC","description":"1234567890123456789012345678901234567890","host":"127.0.0.1","port":1,"groups":["ops"]},
  {"name":"DEVONLY","description":"Dev Only","host":"127.0.0.1","port":9999,"groups":["dev"]}]}
EOF
# Back seed: the backend is a second tn3270proxy instance — a real TN3270
# server we control. Bridged sessions land on its login screen.
cat > "$WORK/back-seed.json" <<EOF
{"groups":["ops"],"users":[{"username":"bob","password":"changeme","groups":["ops"]}],"services":[]}
EOF

"$WORK/tn3270proxy" seed -db "$WORK/front.db" -file "$WORK/front-seed.json" >/dev/null || exit 1
"$WORK/tn3270proxy" seed -db "$WORK/back.db" -file "$WORK/back-seed.json" >/dev/null || exit 1

# Pager seed: user 'pager' (group 'many') sees 22 services, forcing a 2-page
# menu at MOD 2's non-admin capacity (17). Services point at a dead port (never
# bridged — we only page through the list). NAMEs are <=8 A-Z/0-9.
pager_svcs=""
for i in $(seq 1 22); do
  n=$(printf "PAGE%02d" "$i")
  sep=","; [ -z "$pager_svcs" ] && sep=""
  pager_svcs="${pager_svcs}${sep}{\"name\":\"$n\",\"description\":\"Pager Service $i\",\"host\":\"127.0.0.1\",\"port\":1,\"groups\":[\"many\"]}"
done
cat > "$WORK/pager-seed.json" <<EOF
{"groups":["many"],
 "users":[{"username":"pager","password":"changeme","groups":["many"]}],
 "services":[$pager_svcs]}
EOF
"$WORK/tn3270proxy" seed -db "$WORK/front.db" -file "$WORK/pager-seed.json" >/dev/null || exit 1

"$WORK/tn3270proxy" serve -db "$WORK/front.db" -config "$WORK/front-cfg.json" >"$WORK/front.log" 2>&1 &
FRONT_PID=$!
"$WORK/tn3270proxy" serve -db "$WORK/back.db" -config "$WORK/back-cfg.json" >"$WORK/back.log" 2>&1 &
BACK_PID=$!
sleep 1
grep -q listening "$WORK/front.log" || { echo "FAIL: front proxy did not start"; cat "$WORK/front.log"; exit 1; }
grep -q listening "$WORK/back.log"  || { echo "FAIL: back proxy did not start";  cat "$WORK/back.log";  exit 1; }

# A THIRD proxy with a deliberately short post-auth idle (2s) for the idle-logout
# scenario (GH #18). It is isolated so the tiny idle window can't disconnect the
# bridge/PA3 scenarios on the front proxy. Reuses the front seed (alice/ops).
cat > "$WORK/idle-cfg.json" <<EOF
{"listeners":{"plain":{"enabled":true,"addr":"127.0.0.1:$IDLE_PORT"},"tls":{"enabled":false}},
 "limits":{"idle":"2s","pre_auth_idle":"30s","pre_auth_max":"120s"}}
EOF
"$WORK/tn3270proxy" seed -db "$WORK/idle.db" -file "$WORK/front-seed.json" >/dev/null || exit 1
"$WORK/tn3270proxy" serve -db "$WORK/idle.db" -config "$WORK/idle-cfg.json" >"$WORK/idle.log" 2>&1 &
IDLE_PID=$!
sleep 1
grep -q listening "$WORK/idle.log" || { echo "FAIL: idle proxy did not start"; cat "$WORK/idle.log"; exit 1; }

# --- 1. login screen renders; password non-display; cursor on userid field ---
s3 t1 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
Ascii()
ReadBuffer(Ascii)
Quit()
EOF
check "1a login screen renders" "TN3270 GATEWAY LOGIN" "$WORK/t1.out"
# c0=cd = unprotected + non-display (c0 & 0x0C == 0x0C); userid is c0=c1.
check "1b password field is non-display" "SF(c0=cd" "$WORK/t1.out"
# Status line: ... rows cols CURSOR-ROW CURSOR-COL ... — input starts at (3,17),
# one right of the attribute byte (the go3270 field.Col+1 rule).
check "1c cursor lands on userid input" "^U F U C(127.0.0.1) I 2 24 80 3 17 " "$WORK/t1.out"

# --- 2. wrong password: error line, re-prompt, no crash ---
s3 t2 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(alice)
Tab()
String(wrongpass)
Enter()
Wait(5,InputField)
Ascii()
Quit()
EOF
check "2a wrong password shows error line" "Invalid userid or password" "$WORK/t2.out"
check "2b login screen re-prompts" "TN3270 GATEWAY LOGIN" "$WORK/t2.out"

# --- 3. correct login → group-filtered menu ---
s3 t3 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(alice)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
Ascii()
Quit()
EOF
check "3a menu shown after login" "TN3270 GATEWAY MENU" "$WORK/t3.out"
check "3b ops service BACKEND listed" "BACKEND" "$WORK/t3.out"
check "3c ops service DEADHOST listed" "DEADHOST" "$WORK/t3.out"
ncheck "3d dev-only service hidden from alice" "DEVONLY" "$WORK/t3.out"
check "3e status block User ID row" "User ID. :" "$WORK/t3.out"
check "3f status block Release row"  "Release. :" "$WORK/t3.out"
check "3g status block Terminal row" "Terminal :" "$WORK/t3.out"
check "3h wide description renders"        "1234567890123456789012345678901234567890" "$WORK/t3.out"
check "3i status block coexists with wide desc" "User ID. :" "$WORK/t3.out"
# Cursor on the selection input (top "Option ===>" command line, row 1 col 15 on
# a MOD 2 after the #65 band rework); login is the only other screen and it
# reports 3 17, so 1 15 is the menu.
check "3j menu cursor on selection input (1,15)" "I 2 24 80 1 15 " "$WORK/t3.out"

# --- 4. select 1 + ENTER bridges to the backend proxy ---
BACK_CONNS_BEFORE=$(grep -c "accepted connection" "$WORK/back.log")
s3 t4 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(alice)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
String(1)
Enter()
Wait(5,Output)
Wait(5,InputField)
Ascii()
Quit()
EOF
# Bridged session lands on the BACKEND's login screen (menu is gone).
check "4a bridge lands on backend login screen" "TN3270 GATEWAY LOGIN" "$WORK/t4.out"
BACK_CONNS_AFTER=$(grep -c "accepted connection" "$WORK/back.log")
if [ "$BACK_CONNS_AFTER" -gt "$BACK_CONNS_BEFORE" ]; then
  PASS=$((PASS+1)); echo "PASS: 4b backend log shows new accepted connection"
else
  FAIL=$((FAIL+1)); echo "FAIL: 4b no new connection in back.log ($BACK_CONNS_BEFORE -> $BACK_CONNS_AFTER)"
fi

# --- 5. PA3 during bridge returns to the menu, still logged in ---
s3 t5 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(alice)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
String(1)
Enter()
Wait(5,Output)
Wait(5,InputField)
PA(3)
Wait(5,Output)
Wait(5,InputField)
Ascii()
Quit()
EOF
check "5  PA3 returns to menu without re-login" "TN3270 GATEWAY MENU" "$WORK/t5.out"

# --- 6. unreachable service: error on menu, session stays alive ---
s3 t6 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(alice)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
String(2)
Enter()
Wait(5,InputField)
Ascii()
Quit()
EOF
check "6a unreachable service shows error" "Could not connect" "$WORK/t6.out"
check "6b menu redisplayed (session alive)" "TN3270 GATEWAY MENU" "$WORK/t6.out"

# --- 7. PF3 chain: menu → login screen, second PF3 → clean disconnect ---
s3 t7 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(alice)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
PF(3)
Wait(5,InputField)
Ascii()
PF(3)
Wait(5,Disconnect)
Quit()
EOF
check "7a PF3 at menu logs off to login screen" "TN3270 GATEWAY LOGIN" "$WORK/t7.out"
# Wait(Disconnect) emits "error" on timeout if the host never closed.
ncheck "7b second PF3 disconnects cleanly" "^error" "$WORK/t7.out"

# --- 8. empty menu (issue #4): Enter on an empty menu returns control to the
# session, which re-queries and re-renders — the session stays alive instead of
# being trapped. charlie's group has no services, so the menu mapping is empty:
# the SAME branch the store-error path takes (services=nil). This exercises the
# fixed `menuRequery` return through the real go3270 stream — it does NOT
# reproduce the transient store-error itself (inducing that black-box is out of
# scope; the unit tests cover the classification decision). ---
s3 t8 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(charlie)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
Ascii()
Enter()
Wait(5,InputField)
Ascii()
Quit()
EOF
check "8a empty menu shows no-services message" "no services available" "$WORK/t8.out"
# Wait(5,InputField) after the second Enter emits an "error" line on timeout if
# the session were trapped/desynced; its absence proves the menu re-rendered.
ncheck "8b Enter on empty menu does not hang (session not trapped)" "^error" "$WORK/t8.out"
check "8c menu re-renders after Enter (session alive)" "TN3270 GATEWAY MENU" "$WORK/t8.out"

# --- 9. admin cursor positions. admin is a ZZADMIN member with no service
# groups, so the menu shows the "A" entry over an empty service list and the
# selection field accepts letters. Walk: login -> service menu -> admin menu
# -> users list -> add-user form (PF4), capturing the cursor at each stop.
# The empty admin-list (0,0) cursor is NOT exercised here (every entity list is
# non-empty in this seed); TestAdminListScreenCursorEmptyHomes covers it. ---
s3 t9 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(admin)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
Ascii()
String(A)
Enter()
Wait(5,InputField)
Ascii()
String(1)
Enter()
Wait(5,InputField)
Ascii()
PF(4)
Wait(5,InputField)
Ascii()
Quit()
EOF
check "9a admin menu renders" "TN3270 GATEWAY ADMIN" "$WORK/t9.out"
# Admin menu option field at the top "Option ===>" command line, row 1 col 15
# (same row as the service menu — both report 1 15 after the #65 band rework).
check "9b admin/menu cursor on option field (1,15)" "I 2 24 80 1 15 " "$WORK/t9.out"
# Users list: first CMD field at row 4 col 3 — produced only by the list.
check "9c users list cursor on first CMD field (4,3)" "I 2 24 80 4 3 " "$WORK/t9.out"
# Add-user form: first input at row 3 col 17. Login also reports 3 17, so assert
# a 3 17 cursor line that appears AFTER the list's 4 3 line — that one is the
# form, not the earlier login screen.
if awk '/I 2 24 80 4 3 /{seen=1} seen && /I 2 24 80 3 17 /{ok=1} END{exit !ok}' "$WORK/t9.out"; then
  PASS=$((PASS+1)); echo "PASS: 9d add-user form cursor (3,17) after users list"
else
  FAIL=$((FAIL+1)); echo "FAIL: 9d add-user form cursor not at (3,17) after users list"
fi

# --- 10. post-auth idle LOGS OUT to the login screen (GH #18), not disconnect.
# Against the short-idle proxy: log in, reach the menu, then sit idle past the
# 2s post-auth window. The proxy must re-render the LOGIN screen (deauth) with
# the cursor homed to the userid field — the protocol-risk path (a deadline
# firing mid-HandleScreen, then re-driving the login screen). Two Ascii captures
# to one file; awk asserts the menu→login ORDER so the initial login screen
# can't satisfy the check vacuously. ---
s3 t10 <<EOF
Connect(127.0.0.1:$IDLE_PORT)
Wait(5,InputField)
String(alice)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
Ascii()
Wait(4,Output)
Wait(5,InputField)
Ascii()
Quit()
EOF
check "10a reached menu before idle" "TN3270 GATEWAY MENU" "$WORK/t10.out"
# Order proves the transition: MENU captured first, then LOGIN after the idle.
if awk '/TN3270 GATEWAY MENU/{seen=1} seen && /TN3270 GATEWAY LOGIN/{ok=1} END{exit !ok}' "$WORK/t10.out"; then
  PASS=$((PASS+1)); echo "PASS: 10b post-auth idle logs out to login screen"
else
  FAIL=$((FAIL+1)); echo "FAIL: 10b post-auth idle did not return to login screen"
fi
# Cursor homed: menu cursor (1 15) then the re-rendered login cursor (3 17) after.
if awk '/I 2 24 80 1 15 /{seen=1} seen && /I 2 24 80 3 17 /{ok=1} END{exit !ok}' "$WORK/t10.out"; then
  PASS=$((PASS+1)); echo "PASS: 10c login cursor homes to userid after idle-logout"
else
  FAIL=$((FAIL+1)); echo "FAIL: 10c cursor not homed to (3,17) after idle-logout"
fi

# --- 11. System Parameters admin form (GH #44): render, cursor on the form
# field, and PF3 back to the admin menu. Walk: login admin -> service menu ->
# admin menu (A) -> System Parameters (4) -> PF3. The form TITLE is
# "TN3270 GATEWAY ADMIN: SYSTEM PARAMETERS" (a superstring of the admin-menu
# title), so the PF3-return check keys off the admin menu's HELP line
# ("PF3=Main Menu") which the form's help ("Enter = save") can't satisfy. ---
s3 t11 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(admin)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
String(A)
Enter()
Wait(5,InputField)
Ascii()
String(4)
Enter()
Wait(5,InputField)
Ascii()
PF(3)
Wait(5,InputField)
Ascii()
Quit()
EOF
check "11a system parameters form renders" "SYSTEM PARAMETERS" "$WORK/t11.out"
# Labels render with right-aligned-colon dot leaders (GH #71), so the MOTD label
# reads "MOTD File . . . . . . :" rather than a bare "MOTD File:".
check "11b MOTD File label present" "MOTD File ." "$WORK/t11.out"
# Form first input at row 3 col 28 (field.Col+1). The System Parameters form's
# input column is dynamic (GH #71): it sits past the longest label
# ("Auth Fail Window (min):", 23 chars) at attribute col 27, so the cursor lands
# at col 28 — and every field, including MOTD File, aligns there. Assert 3 28
# AFTER the admin menu's 1 15 line so the earlier login screen (3 17) can't match.
if awk '/I 2 24 80 1 15 /{seen=1} seen && /I 2 24 80 3 28 /{ok=1} END{exit !ok}' "$WORK/t11.out"; then
  PASS=$((PASS+1)); echo "PASS: 11c form cursor on first input (3,28) after admin menu"
else
  FAIL=$((FAIL+1)); echo "FAIL: 11c form cursor not at (3,28) after admin menu"
fi
# PF3 on the form returns to the admin menu: the form title appears first, then
# the admin menu's distinctive help line ("PF3=Main Menu", not the form's save).
if awk '/SYSTEM PARAMETERS/{seen=1} seen && /PF3=Main Menu/{ok=1} END{exit !ok}' "$WORK/t11.out"; then
  PASS=$((PASS+1)); echo "PASS: 11d PF3 on form returns to admin menu"
else
  FAIL=$((FAIL+1)); echo "FAIL: 11d PF3 on form did not return to admin menu"
fi

# --- 12. System Parameters Enter-save behavior (GH #44): Enter SAVES IN PLACE
# and stays on the form (it does NOT return to the admin menu); PF3 is the way
# out. Then re-enter the form to prove the value persisted to the store. Walk:
# type a path, Enter (stay), capture; PF3 -> admin menu, capture; re-enter (4),
# capture. The form help line ("Enter = save") vs the admin menu help line
# ("PF3=Main Menu") distinguishes the two screens in the accumulated output. ---
s3 t12 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(admin)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
String(A)
Enter()
Wait(5,InputField)
String(4)
Enter()
Wait(5,InputField)
String(/etc/motd.smoke)
Enter()
Wait(5,InputField)
Ascii()
PF(3)
Wait(5,InputField)
Ascii()
String(4)
Enter()
Wait(5,InputField)
Ascii()
Quit()
EOF
check "12a saved MOTD value present" "/etc/motd.smoke" "$WORK/t12.out"
# Enter stays on the form: the typed value appears BEFORE we ever reach the
# admin menu ("PF3=Main Menu"), i.e. Enter did not pop back to the menu.
if awk '/PF3=Main Menu/{menu=1} /\/etc\/motd\.smoke/ && !menu {ok=1} END{exit !ok}' "$WORK/t12.out"; then
  PASS=$((PASS+1)); echo "PASS: 12b Enter saves in place (stays on form, not back to menu)"
else
  FAIL=$((FAIL+1)); echo "FAIL: 12b Enter did not stay on the form after save"
fi
# Persisted: after PF3 to the admin menu, re-entering rebuilds the form from the
# store and the saved value shows AFTER the admin menu line — proves read-back.
if awk '/PF3=Main Menu/{menu=1} menu && /\/etc\/motd\.smoke/{ok=1} END{exit !ok}' "$WORK/t12.out"; then
  PASS=$((PASS+1)); echo "PASS: 12c saved value persists and pre-populates on re-entry"
else
  FAIL=$((FAIL+1)); echo "FAIL: 12c saved value did not persist on re-entry"
fi

# --- 13. MOTD/NEWS gate (GH #45): the MOTD screen renders after login, pages
# with ENTER, and ignores PA3/PF3 (both silent no-ops on the gate). The MOTD
# screen is all protected text with the cursor homed to {0,0} — no input field —
# so Wait(InputField) may never be satisfied on it; use Wait(Unlock) after AID
# keys. The MOTD file is 25 lines, which on a MOD 2 (22 text lines/page) spans
# TWO pages, so two ENTERs are needed to clear the gate and reach the menu.
#
# A 25-line MOTD fixture: a "*** SYSTEM NEWS ***" banner plus 24 numbered lines.
{ echo "*** SYSTEM NEWS ***"; for i in $(seq 1 24); do echo "NEWS LINE $i"; done; } > "$WORK/motd.txt"
#
# Scenario 12 left MOTD_FILE set to /etc/motd.smoke in front.db; re-point it at
# the real fixture via the System Parameters admin form. The MOTD field is the
# only field on the form (cursor lands on it), so EraseEOF() clears the stale
# value before we type the real path. The path is interpolated into the macro
# here because s3() can't expand $WORK itself.
s3 t13set <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(admin)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
String(A)
Enter()
Wait(5,InputField)
String(4)
Enter()
Wait(5,InputField)
EraseEOF()
String($WORK/motd.txt)
Enter()
Wait(5,InputField)
Quit()
EOF

# 13a/13b: alice logs in, lands on MOTD page 1, presses PA3 then PF3 (both must
# be inert — must NOT advance to the menu), then Quits WITHOUT pressing Enter.
s3 t13a <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(alice)
Tab()
String(changeme)
Enter()
Wait(Unlock)
Wait(1,seconds)
Ascii()
PA(3)
Wait(1,seconds)
Ascii()
PF(3)
Wait(1,seconds)
Ascii()
Quit()
EOF
check  "13a MOTD page 1 shown after login"        "SYSTEM NEWS"          "$WORK/t13a.out"
ncheck "13b menu NOT reached via PA3/PF3 (inert)" "TN3270 GATEWAY MENU"  "$WORK/t13a.out"

# 13c: alice logs in, presses ENTER twice (page1 -> page2 -> menu); the menu
# (an input-field screen) is reached after the gate clears.
s3 t13b <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(alice)
Tab()
String(changeme)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(5,InputField)
Ascii()
Quit()
EOF
check "13c ENTER pages through MOTD to the menu" "TN3270 GATEWAY MENU" "$WORK/t13b.out"

# --- 14. Edit User Details form (GH #46): the user-list `S` line command opens
# a unified edit form whose USERNAME is display-only, so the cursor lands on the
# first EDITABLE field (Full name, row 5) — distinct from every other form's
# (3,17). NOTE: scenario 13 activated the MOTD gate (front.db MOTD_FILE now
# points at a real 2-page fixture), so every login here must clear the gate with
# two ENTERs (Wait(Unlock) after each — the MOTD page has no input field) before
# reaching the service menu. Walk: login admin -> clear MOTD -> A -> users list
# (1) -> S on the first row -> edit form, capture; PF3 -> users list, capture.
# First user by username is ADMIN (ordered ADMIN, ALICE, CHARLIE); editing self
# renders fine and we exit via PF3 without saving. The list legend "S = edit
# user" is unique to the users list and distinguishes it from the edit form. ---
s3 t14 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(admin)
Tab()
String(changeme)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(5,InputField)
String(A)
Enter()
Wait(5,InputField)
String(1)
Enter()
Wait(5,InputField)
Ascii()
String(S)
Enter()
Wait(5,InputField)
Ascii()
PF(3)
Wait(5,InputField)
Ascii()
Quit()
EOF
check "14a edit-user form renders" "EDIT USER" "$WORK/t14.out"
check "14b full name label present" "Full name" "$WORK/t14.out"
check "14c email label present" "Email" "$WORK/t14.out"
# Username is display-only, so the cursor homes to Full name at row 5 col 17.
# Assert a 5 17 cursor line AFTER the users-list 4 3 line so nothing earlier can
# satisfy it vacuously.
if awk '/I 2 24 80 4 3 /{seen=1} seen && /I 2 24 80 5 17 /{ok=1} END{exit !ok}' "$WORK/t14.out"; then
  PASS=$((PASS+1)); echo "PASS: 14d edit-user cursor on Full name (5,17), username read-only"
else
  FAIL=$((FAIL+1)); echo "FAIL: 14d edit-user cursor not at (5,17) after users list"
fi
# PF3 on the form returns to the users list: the edit-form title appears first,
# then the list's distinctive legend ("S = edit user") reappears after it.
if awk '/EDIT USER/{seen=1} seen && /S = edit user/{ok=1} END{exit !ok}' "$WORK/t14.out"; then
  PASS=$((PASS+1)); echo "PASS: 14e PF3 on edit form returns to users list"
else
  FAIL=$((FAIL+1)); echo "FAIL: 14e PF3 on edit form did not return to users list"
fi

# --- 15. User Settings menu entry (GH #63): the service menu shows a "0 User
# Settings" meta-row for every user; selecting "0" opens the User Settings
# screen whose first row is "1 Password" / "Change your sign-on password"; PF3 returns to the service
# menu. Scenario 13 activated the MOTD gate (front.db MOTD_FILE now points at
# a real 2-page fixture), so this login must clear the gate with two ENTERs
# (Wait(Unlock) after each — the MOTD page has no input field) before reaching
# the service menu. Walk: login alice -> clear MOTD (2x Enter) -> service menu
# (check "0 User Settings") -> type "0", Enter -> User Settings screen ->
# assert title + change-password row + cursor at (1,15) -> PF3 -> service menu.
s3 t15 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(alice)
Tab()
String(changeme)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(5,InputField)
Ascii()
String(0)
Enter()
Wait(5,InputField)
Ascii()
PF(3)
Wait(5,InputField)
Ascii()
Quit()
EOF
check "15a service menu shows '0 User Settings' entry" "User Settings" "$WORK/t15.out"
check "15b User Settings screen renders (title)" "USER SETTINGS" "$WORK/t15.out"
check "15c User Settings shows Change Password option" "Change your sign-on password" "$WORK/t15.out"
# Cursor on the option input field at (1,15): FieldUSOption sits on the top
# "Option ===>" command line after the #65 band rework. Assert a 1 15 cursor line
# AFTER the service menu's 1 15 line so the earlier menu rendering can't satisfy
# it vacuously (awk tracks first-seen then second-seen).
if awk '/I 2 24 80 1 15 /{count++} count==2{ok=1; exit} END{exit !ok}' "$WORK/t15.out"; then
  PASS=$((PASS+1)); echo "PASS: 15d User Settings cursor on option field (1,15)"
else
  FAIL=$((FAIL+1)); echo "FAIL: 15d User Settings cursor not at (1,15) after service menu"
fi
# PF3 returns to the service menu: USER SETTINGS appears first, then GATEWAY MENU reappears.
if awk '/USER SETTINGS/{seen=1} seen && /TN3270 GATEWAY MENU/{ok=1} END{exit !ok}' "$WORK/t15.out"; then
  PASS=$((PASS+1)); echo "PASS: 15e PF3 on User Settings returns to service menu"
else
  FAIL=$((FAIL+1)); echo "FAIL: 15e PF3 on User Settings did not return to service menu"
fi

# --- 16. multi-page service menu: pager (22 services) pages with PF7/PF8 ---
# NOTE: scenario 13 activated the MOTD gate (front.db MOTD_FILE now points at a
# real 2-page fixture), so each login below must clear the gate with two ENTERs
# (Wait(Unlock) after each — the MOTD page has no input field) before the menu
# is reached.
s3 t16p1 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(pager)
Tab()
String(changeme)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(5,InputField)
Ascii()
ReadBuffer(Ascii)
Quit()
EOF
check  "16a page 1 indicator" "ITEMS 1 TO 17 OF 22" "$WORK/t16p1.out"
check  "16b page 1 first service"  "PAGE01" "$WORK/t16p1.out"
check  "16c page 1 last on-page service" "PAGE17" "$WORK/t16p1.out"
ncheck "16d page 1 hides overflow service" "PAGE18" "$WORK/t16p1.out"
check  "16e meta entry present on page 1" "User Settings" "$WORK/t16p1.out"
check  "16f menu cursor on selection input (1,15)" "I 2 24 80 1 15 " "$WORK/t16p1.out"

s3 t16p2 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(pager)
Tab()
String(changeme)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(5,InputField)
PF(8)
Wait(5,InputField)
Ascii()
Quit()
EOF
check  "16g PF8 -> page 2 indicator" "ITEMS 18 TO 22 OF 22" "$WORK/t16p2.out"
check  "16h page 2 shows overflow service" "PAGE22" "$WORK/t16p2.out"
ncheck "16i page 2 hides page-1 service" "PAGE01" "$WORK/t16p2.out"
check  "16j meta entry present on page 2" "User Settings" "$WORK/t16p2.out"

# PF8 at the last page is a no-op (still page 2); PF7 then returns to page 1.
s3 t16p3 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(pager)
Tab()
String(changeme)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(5,InputField)
PF(8)
Wait(5,InputField)
PF(8)
Wait(5,InputField)
Ascii()
ReadBuffer(Ascii)
Quit()
EOF
check  "16k PF8 at last page is a no-op" "ITEMS 18 TO 22 OF 22" "$WORK/t16p3.out"

s3 t16p4 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(pager)
Tab()
String(changeme)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(5,InputField)
PF(8)
Wait(5,InputField)
PF(7)
Wait(5,InputField)
Ascii()
Quit()
EOF
check  "16l PF7 returns to page 1" "ITEMS 1 TO 17 OF 22" "$WORK/t16p4.out"

echo
echo "=== $PASS passed, $FAIL failed (evidence in $WORK) ==="
[ "$FAIL" -eq 0 ]
