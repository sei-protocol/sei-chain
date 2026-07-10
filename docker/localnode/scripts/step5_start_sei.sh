#!/usr/bin/env sh

NODE_ID=${ID:-0}
INVARIANT_CHECK_INTERVAL=${INVARIANT_CHECK_INTERVAL:-0}

LOG_DIR="build/generated/logs"
mkdir -p $LOG_DIR

echo "Starting the seid process for node $NODE_ID with invariant check interval=$INVARIANT_CHECK_INTERVAL..."

seid start --chain-id sei --inv-check-period ${INVARIANT_CHECK_INTERVAL} > "$LOG_DIR/seid-$NODE_ID.log" 2>&1 &
SEID_PID=$!
echo "Node $NODE_ID seid is started now"

until seid status >/dev/null 2>&1 && seid q tendermint-validator-set >/dev/null 2>&1
do
  if ! kill -0 "$SEID_PID" 2>/dev/null; then
    echo "seid exited before becoming ready; see $LOG_DIR/seid-$NODE_ID.log"
    exit 1
  fi
  sleep 1
done

echo "Done" >> build/generated/launch.complete
