#!/usr/bin/env bash
# Ping lag_status endpoint every N seconds. When lag>0 print full output; when lag=0 update one line in place.

set -e

URL="${LAG_STATUS_URL:-http://63.179.246.214:26657/lag_status}"
INTERVAL="${PING_INTERVAL:-1}"

echo "Pinging $URL every ${INTERVAL}s (Ctrl+C to stop)"
echo "---"

trap 'echo' EXIT

while true; do
  response=$(curl -sS "$URL")
  lag=$(echo "$response" | jq -r '.lag | tonumber? // 0' 2>/dev/null || echo "0")

  if [ "$lag" -gt 0 ] 2>/dev/null; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')]"
    echo "$response"
    echo ""
    echo "---"
  else
    printf '\r[%s] lag: 0   ' "$(date '+%Y-%m-%d %H:%M:%S')"
  fi

  sleep "$INTERVAL"
done
