#!/usr/bin/env bash
set -euo pipefail

# Ensure build/generated dir exists
mkdir -p build/generated

echo "[INFO] Starting local seid node for integration tests..."

# Start seid in background (adjust flags if your setup differs)
seid start \
  --rpc.laddr tcp://0.0.0.0:26657 \
  --grpc.address 0.0.0.0:9090 \
  --minimum-gas-prices 0.0001usei \
  > build/generated/seid.log 2>&1 &

SEID_PID=$!

# Wait until RPC is alive
echo "[INFO] Waiting for seid RPC to respond..."
ready=false
for i in {1..30}; do
  if curl -s http://localhost:26657/status > /dev/null; then
    echo "[INFO] seid node is up!"
    ready=true
    break
  fi
  echo "[INFO] Attempt $i — seid not ready yet..."
  sleep 2
done

if [ "$ready" = false ]; then
  echo "[ERROR] seid failed to start" >&2
  kill "$SEID_PID" >/dev/null 2>&1 || true
  exit 1
fi

# Write the launch.complete marker
echo "node started at $(date)" > build/generated/launch.complete
echo "[INFO] Wrote build/generated/launch.complete"

# Keep the node running in foreground for Docker CI
wait $SEID_PID
