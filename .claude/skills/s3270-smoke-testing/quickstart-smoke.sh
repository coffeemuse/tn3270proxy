#!/usr/bin/env bash
# quickstart-smoke.sh — verify `quickstart` provisions a usable data dir and that
# `serve` starts cleanly against the generated config. Binary-level (no Docker).
# The 3270 bridge path itself is covered by smoke.sh (bridges to dummy3270).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

BIN="$(mktemp -d)/tn3270proxy"
go build -o "$BIN" ./cmd/tn3270proxy

DATA="$(mktemp -d)"
echo "== provisioning fresh data dir: $DATA"
"$BIN" quickstart -data "$DATA" | tee "$DATA/.out"
grep -q "FRESH INSTALL" "$DATA/.out"

for f in tn3270proxy.json proxy.db mfa.key tls/cert.pem tls/key.pem motd.txt SETUP-DEFAULTS.TXT; do
  test -f "$DATA/$f" || { echo "MISSING artifact: $f"; exit 1; }
done
echo "== all artifacts present"

echo "== re-run must be a no-op"
"$BIN" quickstart -data "$DATA" | grep -qi "existing installation detected"

echo "== serve starts against the generated config"
"$BIN" serve -config "$DATA/tn3270proxy.json" &
SRV=$!
sleep 2
if ! kill -0 "$SRV" 2>/dev/null; then
  echo "serve exited prematurely"; exit 1
fi
kill "$SRV" 2>/dev/null || true
echo "QUICKSTART SMOKE: PASS"
