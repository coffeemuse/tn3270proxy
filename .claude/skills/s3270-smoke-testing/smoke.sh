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
go build -o "$WORK/dummy3270" ./cmd/dummy3270 || { echo "FAIL: build dummy3270"; exit 1; }

# --- configs: each instance gets an explicit -config that DISABLES the TLS
# listener. Without it the repo's tn3270proxy.json is auto-loaded and tries to
# bind :2324, colliding with any already-running proxy. -listen alone does NOT
# disable the file's TLS listener. ---
cat > "$WORK/front-cfg.json" <<EOF
{"listeners":{"plain":{"enabled":true,"addr":"127.0.0.1:$FRONT_PORT"},"tls":{"enabled":false}}}
EOF

# Front seed: alice(ops) sees BACKEND (the dummy3270 backend) + DEADHOST (port 1,
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
"$WORK/tn3270proxy" seed -db "$WORK/front.db" -file "$WORK/front-seed.json" >/dev/null || exit 1

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
"$WORK/dummy3270" -listen 127.0.0.1:$BACK_PORT >"$WORK/back.log" 2>&1 &
BACK_PID=$!
sleep 1
grep -q listening "$WORK/front.log" || { echo "FAIL: front proxy did not start"; cat "$WORK/front.log"; exit 1; }
grep -q listening "$WORK/back.log"  || { echo "FAIL: dummy backend did not start";  cat "$WORK/back.log";  exit 1; }

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

# --- branding setup: login branding renders from the BRANDING document in the
# DB (GH #126), so seed it via the Documents admin flow: admin menu option 8 →
# member list (BRANDING sorts before MOTD, so the cursor home (4,3) IS the
# BRANDING row) → I = import from server file. The import form pre-fills from
# the Branding Import Path sysparam; EraseEOF clears it before typing. A
# successful import returns to the member list with "BRANDING IMPORTED". This
# must run before scenario 1 so the login screen shows branding art. ---
printf '%s\n' "*** SMOKE TEST BRANDING ***" > "$WORK/branding.txt"
s3 t0brand <<EOF
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
String(8)
Enter()
Wait(5,InputField)
String(I)
Enter()
Wait(5,InputField)
EraseEOF()
String($WORK/branding.txt)
Enter()
Wait(5,InputField)
Ascii()
PF(3)
Wait(5,InputField)
PF(3)
Wait(5,InputField)
PF(3)
Wait(5,InputField)
Quit()
EOF
check "0a branding document imported via Documents flow" "BRANDING IMPORTED" "$WORK/t0brand.out"

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
# Status line: ... rows cols CURSOR-ROW CURSOR-COL ... — input starts at
# (Rows-2=22, 16) after the branding-forward layout rework: BodyBottomRow()=22,
# userid attr col=15 so input col=16 (the go3270 field.Col+1 rule).
check "1c cursor lands on userid input" "^U F U C(127.0.0.1) I 2 24 80 22 16 " "$WORK/t1.out"
# Branding line set in t0brand must appear on the login screen body.
check "1d branding line appears on login screen" "SMOKE TEST BRANDING" "$WORK/t1.out"
# Status block: Date and Time labels in the right-hand column (rows 0-1 col 60).
check "1e login status block Date label" "Date . . :" "$WORK/t1.out"
check "1f login status block Time label" "Time . . :" "$WORK/t1.out"

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
check "2a wrong password shows error line" "Invalid user ID or password" "$WORK/t2.out"
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
# a MOD 2 after the #65 band rework); login now reports 22 16 (branding-forward
# layout), so 1 15 unambiguously identifies the menu.
check "3j menu cursor on selection input (1,15)" "I 2 24 80 1 15 " "$WORK/t3.out"

# --- 4. select 1 + ENTER bridges to the dummy backend ---
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
Wait(5,Unlock)
Ascii()
Quit()
EOF
# Bridged session lands on the dummy backend screen (proxy menu is gone).
check "4a bridge lands on dummy backend screen" "DUMMY3270" "$WORK/t4.out"
ncheck "4b bridged screen is not the proxy menu" "TN3270 GATEWAY MENU" "$WORK/t4.out"

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
Wait(5,Unlock)
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
# Add-user form: first input at row 3 col 17. Login now reports 22 16 (branding-
# forward layout), so 3 17 unambiguously identifies the add-user form; assert it
# AFTER the list's 4 3 line to prove ordering.
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
# Cursor homed: menu cursor (1 15) then the re-rendered login cursor (22 16) after
# (branding-forward layout: BodyBottomRow()=22, userid input col=16).
if awk '/I 2 24 80 1 15 /{seen=1} seen && /I 2 24 80 22 16 /{ok=1} END{exit !ok}' "$WORK/t10.out"; then
  PASS=$((PASS+1)); echo "PASS: 10c login cursor homes to userid after idle-logout"
else
  FAIL=$((FAIL+1)); echo "FAIL: 10c cursor not homed to (22,16) after idle-logout"
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
# Labels render with right-aligned-colon dot leaders (GH #71). Document content
# moved into the DB (GH #126), so the path param is now the import-path default:
# the label reads "MOTD Import Path . . :" rather than the old "MOTD File:".
check "11b MOTD Import Path label present" "MOTD Import Path" "$WORK/t11.out"
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
# out. Then re-enter the form to prove the value persisted to the store. The
# cursor homes on the System ID field (first catalog entry); one Tab reaches the
# MOTD Import Path field where the test path is typed. Walk: Tab, type a path,
# Enter (stay), capture; PF3 -> admin menu, capture; re-enter (4), capture. The
# form help line ("Enter = save") vs the admin menu help line ("PF3=Main Menu")
# distinguishes the two screens in the accumulated output. ---
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
Tab()
EraseEOF()
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
# The MOTD renders from the MOTD document in the DB (GH #126), so activate the
# gate by importing the fixture via the Documents admin flow: admin menu 8 →
# member list → Tab twice to the MOTD row (rows sort BRANDING, HELP-MENU, MOTD;
# cursor home is row 0)
# → I = import. The import form pre-fills from the MOTD Import Path sysparam
# (scenario 12 left it at /etc/motd.smoke), so EraseEOF() clears it before
# typing the real fixture path. The path is interpolated into the macro here
# because s3() can't expand $WORK itself.
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
String(8)
Enter()
Wait(5,InputField)
Tab()
Tab()
String(I)
Enter()
Wait(5,InputField)
EraseEOF()
String($WORK/motd.txt)
Enter()
Wait(5,InputField)
Ascii()
Quit()
EOF
check "13z MOTD document imported via Documents flow" "MOTD IMPORTED" "$WORK/t13set.out"

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
# (3,17). NOTE: scenario 13 activated the MOTD gate (the MOTD document in
# front.db now holds a 2-page fixture), so every login here must clear the gate with
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

# --- 15. Settings menu entry (GH #63/#130): the service menu shows a "0 Settings"
# option leading the list on the first page (GH #130 — "User and security
# parameters" description); selecting "0" opens the User Settings
# screen whose first row is "1 Password" / "Change your sign-on password"; PF3 returns to the service
# menu. Scenario 13 activated the MOTD gate (the MOTD document in front.db now
# holds a 2-page fixture), so this login must clear the gate with two ENTERs
# (Wait(Unlock) after each — the MOTD page has no input field) before reaching
# the service menu. Walk: login alice -> clear MOTD (2x Enter) -> service menu
# (check "0 Settings") -> type "0", Enter -> User Settings screen ->
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
check "15a service menu shows '0 Settings' entry leading the list" "User and security parameters" "$WORK/t15.out"
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

# --- 15x. deactivate the MOTD gate for the remaining scenarios: import an
# EMPTY file over the MOTD document (empty content ⇒ PaginateNews returns no
# pages ⇒ the gate is skipped), so the scenario 16-18 logins reach the menu
# directly — matching scenario 3. The admin login here still has to clear the
# 2-page gate from scenario 13 (two ENTERs) before reaching the menu. Scenario
# 19 re-populates the MOTD via the editor and re-verifies the gate at the end.
: > "$WORK/empty.txt"
s3 t15off <<EOF
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
String(8)
Enter()
Wait(5,InputField)
Tab()
Tab()
String(I)
Enter()
Wait(5,InputField)
EraseEOF()
String($WORK/empty.txt)
Enter()
Wait(5,InputField)
Ascii()
Quit()
EOF
check "15f MOTD gate deactivated (empty import, 0 lines)" "MOTD IMPORTED (0 LINES)" "$WORK/t15off.out"

# --- 16. multi-page service menu: pager (22 services) pages with PF7/PF8 ---
# The MOTD gate was deactivated by 15x, so each login reaches the menu
# directly — matching scenario 3.
s3 t16p1 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(pager)
Tab()
String(changeme)
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
check  "16e '0 Settings' leads the list on page 1" "User and security parameters" "$WORK/t16p1.out"
check  "16f menu cursor on selection input (1,15)" "I 2 24 80 1 15 " "$WORK/t16p1.out"

s3 t16p2 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(pager)
Tab()
String(changeme)
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
ncheck "16j '0 Settings' is page-1-only (absent on page 2)" "User and security parameters" "$WORK/t16p2.out"

# PF8 at the last page is a no-op (still page 2); PF7 then returns to page 1.
s3 t16p3 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(pager)
Tab()
String(changeme)
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
Wait(5,InputField)
PF(8)
Wait(5,InputField)
PF(7)
Wait(5,InputField)
Ascii()
Quit()
EOF
check  "16l PF7 returns to page 1" "ITEMS 1 TO 17 OF 22" "$WORK/t16p4.out"

# --- 17. Active Sessions admin screen (GH #91): admin -> A -> 7. The admin's own
# connection is a live session marked *YOU*; the screen renders the column
# headings, homes the cursor to the first command field (4,3), and PF3 returns
# to the admin menu. ---
s3 t17 <<EOF
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
String(7)
Enter()
Wait(5,InputField)
Ascii()
PF(3)
Wait(5,InputField)
Ascii()
Quit()
EOF
check "17a active sessions screen renders"        "ACTIVE SESSIONS" "$WORK/t17.out"
check "17b CLIENT column heading present"          "CLIENT"          "$WORK/t17.out"
check "17c CONNECTED column heading present"        "CONNECTED"       "$WORK/t17.out"
check "17d admin's own session marked *YOU*"        "\*YOU\*"         "$WORK/t17.out"
check "17e cursor on first command field (4,3)"     "I 2 24 80 4 3 "  "$WORK/t17.out"
check "17g legend shows 'S = detail' (D removed from list)" "S = detail"    "$WORK/t17.out"
# PF3 returns to the admin menu: ACTIVE SESSIONS first, then GATEWAY ADMIN again.
if awk '/ACTIVE SESSIONS/{seen=1} seen && /TN3270 GATEWAY ADMIN/{ok=1} END{exit !ok}' "$WORK/t17.out"; then
  PASS=$((PASS+1)); echo "PASS: 17f PF3 on Active Sessions returns to admin menu"
else
  FAIL=$((FAIL+1)); echo "FAIL: 17f PF3 on Active Sessions did not return to admin menu"
fi

# --- 18. Disconnect from the SESSION DETAIL screen (GH #93). A second client
# (alice) connects and sits at the menu, holding its connection open via
# Wait(Disconnect). The admin opens A -> 7, presses S on alice's row (alice has
# the lower session id, so it is row 0) to open the read-only SESSION DETAIL
# screen, then PF11 twice (arm + confirm) to disconnect her; the proxy
# hard-closes alice's connection so her Wait(Disconnect) completes WITHOUT a
# timeout 'error'. PF3 returns to the list. The admin's own session is never the
# target (self-disconnect is vetoed on the detail). ---
sleep 1  # let scenario 17's admin connection fully deregister
s3270 -model 3279-2 > "$WORK/t18alice.out" 2>&1 <<EOF &
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(alice)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
Wait(20,Disconnect)
Ascii()
Quit()
EOF
ALICE18_PID=$!
sleep 2  # alice reaches the menu and registers first (lower id => row 0)
s3 t18admin <<EOF
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
String(7)
Enter()
Wait(5,InputField)
Ascii()
String(S)
Enter()
Wait(5,Output)
Ascii()
PF(11)
Wait(5,Output)
Ascii()
PF(11)
Wait(5,Output)
Ascii()
PF(3)
Wait(5,InputField)
Ascii()
Quit()
EOF
wait "$ALICE18_PID" 2>/dev/null
check  "18a active sessions reached with second client" "ACTIVE SESSIONS"    "$WORK/t18admin.out"
check  "18b session detail screen reached via S"        "SESSION DETAIL"     "$WORK/t18admin.out"
check  "18c detail shows the PF11=Disconnect key"       "PF11=Disconnect"    "$WORK/t18admin.out"
check  "18d disconnect confirm prompt on the detail"    "CONFIRM DISCONNECT" "$WORK/t18admin.out"
check  "18e detail shows DISCONNECTED status"           "DISCONNECTED"       "$WORK/t18admin.out"
check  "18f admin's own row still marked *YOU* on list" "\*YOU\*"            "$WORK/t18admin.out"
# The authoritative proof: alice's held connection was dropped by the admin's
# Disconnect-from-detail (Wait(Disconnect) returns cleanly; a timeout emits '^error').
ncheck "18g alice connection dropped by admin disconnect" "^error"           "$WORK/t18alice.out"
# PF3 from the detail returns to the (refreshed) list: DISCONNECTED renders on the
# detail, then ACTIVE SESSIONS appears again after it (ordering proves the return).
if awk '/DISCONNECTED/{seen=1} seen && /ACTIVE SESSIONS/{ok=1} END{exit !ok}' "$WORK/t18admin.out"; then
  PASS=$((PASS+1)); echo "PASS: 18h PF3 from detail returns to the active-sessions list"
else
  FAIL=$((FAIL+1)); echo "FAIL: 18h PF3 from detail did not return to the list"
fi

# --- 19. Documents admin flow (GH #126): member list, line editor, insert-mode
# regression guard, prefix commands, PF12 cancel, and the saved MOTD rendering
# on the NEWS screen at the next login.
#
# State on entry: BRANDING holds the smoke art (t0brand import); MOTD is EMPTY
# (15x imported a 0-line file), so the admin login reaches the menu directly and
# the MOTD editor opens with a single empty line.
#
# Editor row layout contract (buildEditorScreen): prefix attribute col 0, prefix
# input cols 1-2, text attribute col 3, text cols 4-79. Editable text is written
# UNPADDED so the field's trailing positions stay NUL — native 3270 insert mode
# depends on it (padding with spaces locks the keyboard on Insert).
#
# Walk: login admin -> A -> 8 (member list; capture) -> Tab twice to the MOTD
# row (HELP-MENU sits between BRANDING and MOTD) ->
# E (editor; capture: title, ruler, cursor on the first prefix field (4,1)) ->
# Tab to the text field -> type line one -> PF3 (save; capture "MOTD SAVED" +
# Lines column = 1) -> E again -> Tab, Right x5 (mid-line), Insert(), type X
# (insert-mode guard: must NOT lock the keyboard; shifted content reads back) ->
# Reset, Home -> prefix I on line 1, Enter (blank line appears: ROW 1 TO 2 OF 2)
# -> Tab Tab to line 2's prefix -> D, Enter (back to 1 line) -> PF12 (cancel: no
# save message; awk asserts exactly ONE "MOTD SAVED" in the whole session).
s3 t19 <<EOF
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
String(8)
Enter()
Wait(5,InputField)
Ascii()
Tab()
Tab()
String(E)
Enter()
Wait(5,InputField)
Ascii()
Tab()
String("  SMOKE EDIT LINE ONE")
PF(3)
Wait(5,InputField)
Ascii()
Tab()
Tab()
String(E)
Enter()
Wait(5,InputField)
Tab()
Right()
Right()
Right()
Right()
Right()
Right()
Right()
Insert()
String(X)
Ascii()
Reset()
Home()
String(I)
Enter()
Wait(5,InputField)
Ascii()
Tab()
Tab()
String(D)
Enter()
Wait(5,InputField)
Ascii()
PF(12)
Wait(5,InputField)
Ascii()
Quit()
EOF
check "19a Documents member list renders" "TN3270 GATEWAY ADMIN: DOCUMENTS" "$WORK/t19.out"
check "19b member list shows BRANDING row" "BRANDING" "$WORK/t19.out"
check "19c member list shows MOTD row" "MOTD" "$WORK/t19.out"
check "19c2 member list shows HELP-MENU row" "HELP-MENU" "$WORK/t19.out"
check "19d member list cursor on first CMD field (4,3)" "I 2 24 80 4 3 " "$WORK/t19.out"
check "19e editor title EDIT MOTD" "EDIT MOTD" "$WORK/t19.out"
# The ISPF column ruler row (blue, cols 4-79) over the text area. (grep -- :
# the pattern starts with a dash, so the plain check() helper can't take it.)
if grep -q -- "----+----1----+----2" "$WORK/t19.out"; then
  PASS=$((PASS+1)); echo "PASS: 19f editor column ruler present"
else
  FAIL=$((FAIL+1)); echo "FAIL: 19f editor column ruler not found"
fi
# Editor cursor on the first prefix input: attr col 0 => input col 1 (row 4).
# Assert it AFTER the member list's 4 3 cursor line so ordering is proven.
if awk '/I 2 24 80 4 3 /{seen=1} seen && /I 2 24 80 4 1 /{ok=1} END{exit !ok}' "$WORK/t19.out"; then
  PASS=$((PASS+1)); echo "PASS: 19g editor cursor on first prefix field (4,1)"
else
  FAIL=$((FAIL+1)); echo "FAIL: 19g editor cursor not at (4,1) after member list"
fi
check "19h PF3 saves back to member list" "MOTD SAVED" "$WORK/t19.out"
# Lines column: "%-10s %5d" => "MOTD" + 11 spaces + "1" after the 1-line save.
check "19i MOTD Lines column reflects saved content" "MOTD \{11\}1" "$WORK/t19.out"
# Insert-mode regression guard (trailing-NUL contract): inserting mid-line must
# shift the tail right, not lock the keyboard. A padded text field would lock
# (operator error -> String fails with an 'error' line and the X never lands).
check "19j insert mode shifts content (no keyboard lock)" "SMOKEX EDIT LINE ONE" "$WORK/t19.out"
ncheck "19k no s3270 action errors (keyboard never locked)" "^error" "$WORK/t19.out"
# Prefix I inserted a blank line: the editor row indicator goes to 2 lines.
# Title and RowInfo share screen row 0, so a single-line match ("EDIT MOTD ...
# ROW x TO y") is unambiguous — the member list's own "ROW 1 TO 3 OF 3" (three
# documents) sits on the DOCUMENTS title line and can't satisfy it.
check "19l prefix I inserts a line (editor ROW 1 TO 2 OF 2)" "EDIT MOTD.*ROW 1 TO 2 OF 2" "$WORK/t19.out"
# Prefix D removed it again: an editor 1-line indicator AFTER the 2-line one
# (the editor also shows 1 OF 1 before the insert, so ordering is required).
if awk '/EDIT MOTD.*ROW 1 TO 2 OF 2/{seen=1} seen && /EDIT MOTD.*ROW 1 TO 1 OF 1/{ok=1} END{exit !ok}' "$WORK/t19.out"; then
  PASS=$((PASS+1)); echo "PASS: 19m prefix D deletes the line (editor back to ROW 1 TO 1 OF 1)"
else
  FAIL=$((FAIL+1)); echo "FAIL: 19m prefix D did not shrink the editor back to one line"
fi
# PF12 cancels: back at the member list with NO save message — the only
# "MOTD SAVED" in the whole session is walk A's PF3 save.
if [ "$(grep -c "MOTD SAVED" "$WORK/t19.out")" -eq 1 ]; then
  PASS=$((PASS+1)); echo "PASS: 19n PF12 cancel returns to member list without a save message"
else
  FAIL=$((FAIL+1)); echo "FAIL: 19n PF12 cancel produced an unexpected save message"
fi
# PF12 lands back on the member list: the DOCUMENTS title renders again AFTER
# the editor's 2-line state (the only list render after that point is PF12's).
if awk '/EDIT MOTD.*ROW 1 TO 2 OF 2/{seen=1} seen && /DOCUMENTS/{ok=1} END{exit !ok}' "$WORK/t19.out"; then
  PASS=$((PASS+1)); echo "PASS: 19o PF12 returns to the member list"
else
  FAIL=$((FAIL+1)); echo "FAIL: 19o PF12 did not return to the member list"
fi

# The saved MOTD ("  SMOKE EDIT LINE ONE" with a deliberate 2-space indent,
# saved by walk A's PF3; walk B's edits were cancelled) renders on the NEWS
# screen at the next login: alice lands on the 1-page gate (the text shows
# BEFORE the menu), then one ENTER clears it.
s3 t19news <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(alice)
Tab()
String(changeme)
Enter()
Wait(Unlock)
Wait(1,seconds)
Ascii()
Enter()
Wait(5,InputField)
Ascii()
Quit()
EOF
# Ordering: the saved text appears before the menu title ever does (gate first).
if awk '/TN3270 GATEWAY MENU/{menu=1} /SMOKE EDIT LINE ONE/ && !menu {ok=1} END{exit !ok}' "$WORK/t19news.out"; then
  PASS=$((PASS+1)); echo "PASS: 19p saved MOTD text renders on the NEWS screen"
else
  FAIL=$((FAIL+1)); echo "FAIL: 19p saved MOTD text not on the NEWS screen before the menu"
fi
# Walk B's cancelled insert (the X) must NOT have been persisted.
ncheck "19q PF12-cancelled edit not persisted" "SMOKEX" "$WORK/t19news.out"
check "19r ENTER clears the gate to the menu" "TN3270 GATEWAY MENU" "$WORK/t19news.out"
# Leading-whitespace round trip (KeepSpaces contract): go3270 TrimSpaces field
# values unless the editor's text fields set KeepSpaces, which silently
# destroys indents/ASCII-art positioning on save. NEWS renders MOTD lines at
# col 0 verbatim, so the typed 2-space indent must survive to the line start.
# Found in human QA; unit tests feed synthetic Responses and cannot catch it.
# Anchor anatomy: "data: " (s3270 Ascii row prefix) + 1 blank (the NEWS field
# attribute byte at col 0) + the 2-space indent = exactly 4 spaces before SMOKE.
# Without KeepSpaces the indent is eaten and only 2 spaces remain — no match.
check "19s leading whitespace survives editor save" "^data:    SMOKE EDIT LINE ONE" "$WORK/t19news.out"

# --- 20. PF1 help viewer (GH #132): the stock HELP-MENU document (26 lines →
# 2 pages at MOD 2's 20-line help page size) opens from the service menu. The
# viewer is all protected text with the cursor homed to {0,0} — no input field
# — so use Wait(Unlock)+Wait(1,seconds) after AID keys, like the MOTD gate.
# Uses the pager user ON MENU PAGE 2 so the return check proves the menu page
# survives the help round-trip. NOTE: scenario 19 re-populated the MOTD (1
# line = a 1-page gate), so the login needs one ENTER to clear it. ---
s3 t20 <<EOF
Connect(127.0.0.1:$FRONT_PORT)
Wait(5,InputField)
String(pager)
Tab()
String(changeme)
Enter()
Wait(Unlock)
Wait(1,seconds)
Enter()
Wait(5,InputField)
PF(8)
Wait(5,InputField)
PF(1)
Wait(Unlock)
Wait(1,seconds)
Ascii()
PF(8)
Wait(Unlock)
Wait(1,seconds)
Ascii()
PF(8)
Wait(Unlock)
Wait(1,seconds)
Ascii()
PF(3)
Wait(5,InputField)
Ascii()
Quit()
EOF
check  "20a PF1 opens the help viewer"      "SERVICE MENU HELP"    "$WORK/t20.out"
check  "20b help page 1 indicator"          "PAGE 1 OF 2"          "$WORK/t20.out"
check  "20c help page 1 stock content"      "SELECTING A SERVICE"  "$WORK/t20.out"
check  "20d help cursor homed (0,0)"        "I 2 24 80 0 0 "       "$WORK/t20.out"
check  "20e PF8 pages to help page 2"       "PAGE 2 OF 2"          "$WORK/t20.out"
check  "20f help page 2 stock content"      "OTHER MENU ENTRIES"   "$WORK/t20.out"
ncheck "20g PF8 clamps at the last page"    "PAGE 3 OF"            "$WORK/t20.out"
# The menu was on page 2 (ITEMS 18 TO 22) before PF1; PF3 must restore it.
check  "20h PF3 returns to menu page 2"     "ITEMS 18 TO 22 OF 22" "$WORK/t20.out"
check  "20i menu help row advertises PF1"   "PF1=Help"             "$WORK/t20.out"

echo
echo "=== $PASS passed, $FAIL failed (evidence in $WORK) ==="
[ "$FAIL" -eq 0 ]
