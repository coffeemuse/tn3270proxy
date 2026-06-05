#!/usr/bin/env bash
# Automated MFA smoke test for tn3270proxy (GH #47), driven by s3270.
# Run from the repo root:  .claude/skills/s3270-smoke-testing/mfa-smoke.sh
#
# Covers the protocol surface of the two new MFA screens that unit tests can't:
# enrollment-screen render + cursor + PF3, verification-screen render + cursor,
# a successful code submission reaching the menu, a wrong-code re-prompt, and
# PF3-cancel-to-login. The deterministic bits (a known secret, live TOTP codes)
# come from the mfahelper command under this skill dir.
set -u

PORT="${MFA_PORT:-3431}"
WORK="$(mktemp -d /tmp/s3270mfa.XXXXXX)"
PASS=0 FAIL=0
SECRET="JBSWY3DPEHPK3PXP" # fixed 16-char base32 for the enrolled user

cleanup() { [ -n "${PID:-}" ] && kill "$PID" 2>/dev/null; }
trap cleanup EXIT

check() { # check <name> <grep-BRE> <file>
  if grep -q "$2" "$3"; then PASS=$((PASS+1)); echo "PASS: $1"
  else FAIL=$((FAIL+1)); echo "FAIL: $1 (pattern '$2' not found in $3)"; fi
}

s3() { s3270 -model 3279-2 > "$WORK/$1.out" 2>&1; }

go build -o "$WORK/tn3270proxy" ./cmd/tn3270proxy || { echo "FAIL: build"; exit 1; }
helper() { go run ./.claude/skills/s3270-smoke-testing/mfahelper "$@"; }

# 32-byte base64 master key, exported for serve's startup key check + enroll.
KEY_B64="$(head -c 32 /dev/urandom | base64 | tr -d '\n')"
export TN3270PROXY_MFA_KEY="$KEY_B64"

cat > "$WORK/cfg.json" <<EOF
{"listeners":{"plain":{"enabled":true,"addr":"127.0.0.1:$PORT"},"tls":{"enabled":false}}}
EOF

# enrolluser = required, not yet enrolled (pending → enrollment screen).
# verifyuser = enrolled with the known SECRET (→ verification screen).
cat > "$WORK/seed.json" <<EOF
{"groups":["ops"],
 "users":[
  {"username":"enrolluser","password":"changeme","groups":["ops"]},
  {"username":"verifyuser","password":"changeme","groups":["ops"]}],
 "services":[
  {"name":"BACKEND","description":"Backend Host","host":"127.0.0.1","port":1,"groups":["ops"]}]}
EOF

"$WORK/tn3270proxy" seed -db "$WORK/db" -file "$WORK/seed.json" >/dev/null || exit 1

# Provision MFA state BEFORE serve opens the DB (avoids concurrent writers).
helper setrequired "$WORK/db" enrolluser || { echo "FAIL: setrequired"; exit 1; }
helper enroll "$WORK/db" verifyuser "$KEY_B64" "$SECRET" || { echo "FAIL: enroll"; exit 1; }

"$WORK/tn3270proxy" serve -db "$WORK/db" -config "$WORK/cfg.json" >"$WORK/serve.log" 2>&1 &
PID=$!
sleep 1
grep -q listening "$WORK/serve.log" || { echo "FAIL: proxy did not start"; cat "$WORK/serve.log"; exit 1; }

# --- MFA-1: enrollment screen renders after a pending user logs in ---
s3 t1 <<EOF
Connect(127.0.0.1:$PORT)
Wait(5,InputField)
String(enrolluser)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
Ascii()
Quit()
EOF
check "1a enrollment screen shown"        "MFA ENROLLMENT" "$WORK/t1.out"
check "1b enrollment shows the key label" "Key:"           "$WORK/t1.out"
check "1c confirmation-code prompt shown" "Confirmation code" "$WORK/t1.out"
check "1d enrollment cursor on code field (11,26)" "I 2 24 80 11 26 " "$WORK/t1.out"
# The otpauth URI must NOT be shown (manual entry is the 3270 path).
if grep -q "otpauth://" "$WORK/t1.out"; then
  FAIL=$((FAIL+1)); echo "FAIL: 1e otpauth URI unexpectedly shown"
else PASS=$((PASS+1)); echo "PASS: 1e otpauth URI not shown"; fi

# --- MFA-2: PF3 on the enrollment screen returns to the login screen ---
s3 t2 <<EOF
Connect(127.0.0.1:$PORT)
Wait(5,InputField)
String(enrolluser)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
PF(3)
Wait(5,InputField)
Ascii()
Quit()
EOF
check "2a PF3 on enrollment returns to login" "TN3270 GATEWAY LOGIN" "$WORK/t2.out"

# --- MFA-3: verification screen renders for an enrolled user ---
s3 t3 <<EOF
Connect(127.0.0.1:$PORT)
Wait(5,InputField)
String(verifyuser)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
Ascii()
Quit()
EOF
check "3a verification screen shown"             "MFA VERIFICATION" "$WORK/t3.out"
check "3b verification cursor on code field (4,13)" "I 2 24 80 4 13 " "$WORK/t3.out"

# --- MFA-4: a wrong code re-prompts with an error (session stays on verify) ---
s3 t4 <<EOF
Connect(127.0.0.1:$PORT)
Wait(5,InputField)
String(verifyuser)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
String(000000)
Enter()
Wait(5,InputField)
Ascii()
Quit()
EOF
check "4a wrong code shows error"        "Code incorrect" "$WORK/t4.out"
check "4b wrong code stays on verify"    "MFA VERIFICATION" "$WORK/t4.out"

# --- MFA-5: a correct code reaches the menu ---
# Compute the live code right before submitting so it is within the time step.
CODE="$(helper totp "$SECRET")"
s3 t5 <<EOF
Connect(127.0.0.1:$PORT)
Wait(5,InputField)
String(verifyuser)
Tab()
String(changeme)
Enter()
Wait(5,InputField)
String($CODE)
Enter()
Wait(5,InputField)
Ascii()
Quit()
EOF
check "5a correct code reaches the menu" "TN3270 GATEWAY MENU" "$WORK/t5.out"
# The audit trail lives in the store, not the log; query it directly.
"$WORK/tn3270proxy" audit list -db "$WORK/db" > "$WORK/audit.out" 2>&1
check "5b mfa_enrolled+success audited"  "mfa_success"         "$WORK/audit.out"

echo
echo "=== $PASS passed, $FAIL failed (evidence in $WORK) ==="
[ "$FAIL" -eq 0 ]
