#!/usr/bin/env bash
set -euo pipefail

ENDPOINT=${ENDPOINT:?set ENDPOINT to the S2S resolution endpoint}
PARTNER=${PARTNER:?set PARTNER to the IntentIQ partner id}
ITERATIONS=${ITERATIONS:-10000}
VUS=${VUS:-16}
TIMEOUT=${TIMEOUT:-400ms}
HOOK_TIMEOUT_MS=${HOOK_TIMEOUT_MS:-300}
MAX_BACKGROUND=${MAX_BACKGROUND:-2000}
PORT=${PORT:-8081}
OUT=${OUT:-./results}
CASES=${CASES:-"1 2 3 4 5 6"}

scripts_dir=$(cd "$(dirname "$0")" && pwd)
benchmark_dir=$(cd "$scripts_dir/.." && pwd)
module=$(cd "$benchmark_dir/../.." && pwd)
FIXTURE=${FIXTURE:-$benchmark_dir/testdata/sample.jsonl}
K6_SCRIPT=${K6_SCRIPT:-$benchmark_dir/k6/enrichment.js}
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)

binary="$OUT/benchmark-server"
( cd "$module/cmd/benchmark" && go build -o "$binary" . )

case_spec() {
  case "$1" in
    1) echo "1-sync sync 0 0" ;;
    2) echo "2-sync-wall sync 0 $HOOK_TIMEOUT_MS" ;;
    3) echo "3-async async 0 $HOOK_TIMEOUT_MS" ;;
    4) echo "4-hybrid-250 hybrid 250 $HOOK_TIMEOUT_MS" ;;
    5) echo "5-hybrid-100 hybrid 100 $HOOK_TIMEOUT_MS" ;;
    6) echo "6-hybrid-50 hybrid 50 $HOOK_TIMEOUT_MS" ;;
    *) echo "unknown case $1" >&2; exit 1 ;;
  esac
}

wait_for_port_free() {
  for _ in $(seq 1 40); do
    nc -z 127.0.0.1 "$PORT" >/dev/null 2>&1 || return 0
    sleep 0.25
  done
  echo "port $PORT still busy" >&2
  exit 1
}

for number in $CASES; do
  read -r name mode wait_ms hook_ms <<<"$(case_spec "$number")"
  directory="$OUT/$name"
  rm -rf "$directory"
  mkdir -p "$directory/cache"

  wait_for_port_free
  "$binary" \
      -listen ":$PORT" \
      -endpoint "$ENDPOINT" \
      -partner "$PARTNER" \
      -timeout "$TIMEOUT" \
      -concurrency "$VUS" \
      -cache "$directory/cache" \
      -max-background-calls "$MAX_BACKGROUND" \
      > "$directory/server.log" 2>&1 &
  server=$!
  trap 'kill $server 2>/dev/null || true' EXIT

  for _ in $(seq 1 60); do
    curl -sf "http://127.0.0.1:$PORT/health" >/dev/null && break
    sleep 0.5
  done

  echo "=== $name (mode=$mode wait_ms=$wait_ms hook_ms=$hook_ms)"

  [ "$mode" = "hybrid" ] && wait_env="$wait_ms" || wait_env=1
  MODE="$mode" WAIT_MS="$wait_env" HOOK_TIMEOUT_MS="$hook_ms" \
    ITERATIONS="$ITERATIONS" VUS="$VUS" FIXTURE="$FIXTURE" \
    TARGET_URL="http://127.0.0.1:$PORT/enrich" MAX_DURATION="${MAX_DURATION:-30m}" \
    k6 run --summary-export "$directory/k6.json" \
      --summary-trend-stats "avg,med,p(90),p(95),p(99),max" \
      "$K6_SCRIPT" > "$directory/k6.log" 2>&1 || true

  curl -sf "http://127.0.0.1:$PORT/metrics" > "$directory/metrics.txt"
  tail -3 "$directory/k6.log" || true

  kill "$server" 2>/dev/null || true
  wait "$server" 2>/dev/null || true
  trap - EXIT
done

python3 "$scripts_dir/summarize.py" "$OUT"
