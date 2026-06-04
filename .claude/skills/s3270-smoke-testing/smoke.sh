#!/usr/bin/env bash
# Automated TN3270 smoke test for tn3270proxy, driven by s3270.
# Run from the repo root:  .claude/skills/s3270-smoke-testing/smoke.sh
# Covers the Task 15 manual checklist (login render, non-display password,
# bad password, group-filtered menu, bridge, PA3, unreachable service, PF3).
# Override ports with FRONT_PORT / BACK_PORT env vars if the defaults are taken.
set -u

FRONT_PORT="${FRONT_PORT:-3411}"
BACK_PORT="${BACK_PORT:-3412}"
WORK="$(mktemp -d /tmp/s3270smoke.XXXXXX)"
PASS=0 FAIL=0

cleanup() {
  [ -n "${FRONT_PID:-}" ] && kill "$FRONT_PID" 2>/dev/null
  [ -n "${BACK_PID:-}" ] && kill "$BACK_PID" 2>/dev/null
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
  {"username":"charlie","password":"changeme","groups":["empty"]}],
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

echo
echo "=== $PASS passed, $FAIL failed (evidence in $WORK) ==="
[ "$FAIL" -eq 0 ]
