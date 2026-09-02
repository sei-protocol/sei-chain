#!/usr/bin/env sh

NODE_ID=${ID:-0}
INVARIANT_CHECK_INTERVAL=${INVARIANT_CHECK_INTERVAL:-0}
FREEZE_HEIGHT=${FREEZE_HEIGHT:-0}

LOG_DIR="build/generated/logs"
mkdir -p $LOG_DIR

echo "Starting the seid process for node $NODE_ID with invariant check interval=$INVARIANT_CHECK_INTERVAL and freeze height=$FREEZE_HEIGHT..."

seid start --chain-id sei --inv-check-period "${INVARIANT_CHECK_INTERVAL}" --freeze-height "${FREEZE_HEIGHT}" > "$LOG_DIR/seid-$NODE_ID.log" 2>&1 &
SEID_PID=$!
echo "Node $NODE_ID seid is started now"

# launch.complete means the node's query surface is available, not merely that
# the process has started.
node_query_ready() {
  if [ "${AUTOBAHN_EVMONLY_IN_MEMORY:-false}" = "true" ]; then
    curl -fsS -X POST \
      -H 'content-type: application/json' \
      --data '{"jsonrpc":"2.0","id":1,"method":"eth_sendRawTransaction","params":["0x01"]}' \
      http://127.0.0.1:8545 >/dev/null 2>&1
  else
    seid q tendermint-validator-set >/dev/null 2>&1
  fi
}

until node_query_ready
do
  if ! kill -0 "$SEID_PID" 2>/dev/null; then
    echo "seid exited before becoming ready; see $LOG_DIR/seid-$NODE_ID.log"
    exit 1
  fi
  sleep 1
done

echo "Done" >> build/generated/launch.complete
