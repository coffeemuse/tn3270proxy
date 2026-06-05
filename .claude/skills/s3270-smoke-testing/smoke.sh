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
  {"name":"DEVONLY","description":"Dev Only","host":"127.0.0.1","port":9999,"groups":["dev"]}]}
EOF
# Back seed: the backend is a second tn3270proxy instance — a real TN3270
# server we control. Bridged sessions land on its login screen.
cat > "$WORK/back-seed.json" <<EOF
{"groups":["ops"],"users":[{"username":"bob","password":"changeme","groups":["ops"]}],"services":[]}
EOF

"$WORK/tn3270proxy" seed -db "$WORK/front.db" -file "$WORK/front-seed.json" >/dev/null || exit 1
"$WORK/tn3270proxy" seed -db "$WORK/back.db" -file "$WORK/back-seed.json" >/dev/null || exit 1

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
# Cursor on the selection input (===> field at row 19 col 8 on a MOD 2);
# login is the only other screen and it reports 3 17, so 19 8 is the menu.
check "3e menu cursor on selection input" "I 2 24 80 19 8 " "$WORK/t3.out"

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
# Admin menu option field at row 19 col 8 (same row as the service menu — both
# are correct; the service menu also reports 19 8 in this run).
check "9b admin/menu cursor on option field (19,8)" "I 2 24 80 19 8 " "$WORK/t9.out"
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
# Cursor homed: menu cursor (19 8) then the re-rendered login cursor (3 17) after.
if awk '/I 2 24 80 19 8 /{seen=1} seen && /I 2 24 80 3 17 /{ok=1} END{exit !ok}' "$WORK/t10.out"; then
  PASS=$((PASS+1)); echo "PASS: 10c login cursor homes to userid after idle-logout"
else
  FAIL=$((FAIL+1)); echo "FAIL: 10c cursor not homed to (3,17) after idle-logout"
fi

# --- 11. System Parameters admin form (GH #44): render, cursor on the form
# field, and PF3 back to the admin menu. Walk: login admin -> service menu ->
# admin menu (A) -> System Parameters (4) -> PF3. The form TITLE is
# "TN3270 GATEWAY ADMIN: SYSTEM PARAMETERS" (a superstring of the admin-menu
# title), so the PF3-return check keys off the admin menu's HELP line
# ("Enter = select") which the form's help ("Enter = save") can't satisfy. ---
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
check "11b MOTD File label present" "MOTD File:" "$WORK/t11.out"
# Form first input at row 3 col 17 (field.Col+1). Assert a 3 17 cursor AFTER the
# admin menu's 19 8 line so the earlier login screen (also 3 17) can't satisfy it.
if awk '/I 2 24 80 19 8 /{seen=1} seen && /I 2 24 80 3 17 /{ok=1} END{exit !ok}' "$WORK/t11.out"; then
  PASS=$((PASS+1)); echo "PASS: 11c form cursor on MOTD field (3,17) after admin menu"
else
  FAIL=$((FAIL+1)); echo "FAIL: 11c form cursor not at (3,17) after admin menu"
fi
# PF3 on the form returns to the admin menu: the form title appears first, then
# the admin menu's distinctive help line ("Enter = select", not the form's save).
if awk '/SYSTEM PARAMETERS/{seen=1} seen && /Enter = select/{ok=1} END{exit !ok}' "$WORK/t11.out"; then
  PASS=$((PASS+1)); echo "PASS: 11d PF3 on form returns to admin menu"
else
  FAIL=$((FAIL+1)); echo "FAIL: 11d PF3 on form did not return to admin menu"
fi

# --- 12. System Parameters Enter-save behavior (GH #44): Enter SAVES IN PLACE
# and stays on the form (it does NOT return to the admin menu); PF3 is the way
# out. Then re-enter the form to prove the value persisted to the store. Walk:
# type a path, Enter (stay), capture; PF3 -> admin menu, capture; re-enter (4),
# capture. The form help line ("Enter = save") vs the admin menu help line
# ("Enter = select") distinguishes the two screens in the accumulated output. ---
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
# admin menu ("Enter = select"), i.e. Enter did not pop back to the menu.
if awk '/Enter = select/{menu=1} /\/etc\/motd\.smoke/ && !menu {ok=1} END{exit !ok}' "$WORK/t12.out"; then
  PASS=$((PASS+1)); echo "PASS: 12b Enter saves in place (stays on form, not back to menu)"
else
  FAIL=$((FAIL+1)); echo "FAIL: 12b Enter did not stay on the form after save"
fi
# Persisted: after PF3 to the admin menu, re-entering rebuilds the form from the
# store and the saved value shows AFTER the admin menu line — proves read-back.
if awk '/Enter = select/{menu=1} menu && /\/etc\/motd\.smoke/{ok=1} END{exit !ok}' "$WORK/t12.out"; then
  PASS=$((PASS+1)); echo "PASS: 12c saved value persists and pre-populates on re-entry"
else
  FAIL=$((FAIL+1)); echo "FAIL: 12c saved value did not persist on re-entry"
fi

# --- 13. Edit User Details form (GH #46): the user-list `S` line command opens
# a unified edit form whose USERNAME is display-only, so the cursor lands on the
# first EDITABLE field (Full name, row 5) — distinct from every other form's
# (3,17). Walk: login admin -> A -> users list (1) -> S on the first row -> edit
# form, capture; PF3 -> users list, capture. First user by username is ADMIN
# (ordered ADMIN, ALICE, CHARLIE); editing self renders fine and we exit via PF3
# without saving. The list legend "S = edit user" is unique to the users list and
# distinguishes it from the edit form (help "Enter = save"). ---
s3 t13 <<EOF
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
check "13a edit-user form renders" "EDIT USER" "$WORK/t13.out"
check "13b full name label present" "Full name" "$WORK/t13.out"
check "13c email label present" "Email" "$WORK/t13.out"
# Username is display-only, so the cursor homes to Full name at row 5 col 17.
# Assert a 5 17 cursor line AFTER the users-list 4 3 line so nothing earlier can
# satisfy it vacuously.
if awk '/I 2 24 80 4 3 /{seen=1} seen && /I 2 24 80 5 17 /{ok=1} END{exit !ok}' "$WORK/t13.out"; then
  PASS=$((PASS+1)); echo "PASS: 13d edit-user cursor on Full name (5,17), username read-only"
else
  FAIL=$((FAIL+1)); echo "FAIL: 13d edit-user cursor not at (5,17) after users list"
fi
# PF3 on the form returns to the users list: the edit-form title appears first,
# then the list's distinctive legend ("S = edit user") reappears after it.
if awk '/EDIT USER/{seen=1} seen && /S = edit user/{ok=1} END{exit !ok}' "$WORK/t13.out"; then
  PASS=$((PASS+1)); echo "PASS: 13e PF3 on edit form returns to users list"
else
  FAIL=$((FAIL+1)); echo "FAIL: 13e PF3 on edit form did not return to users list"
fi

echo
echo "=== $PASS passed, $FAIL failed (evidence in $WORK) ==="
[ "$FAIL" -eq 0 ]
