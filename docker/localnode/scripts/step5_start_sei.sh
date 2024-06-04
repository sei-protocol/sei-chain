#!/usr/bin/env sh

NODE_ID=${ID:-0}
INVARIANT_CHECK_INTERVAL=${INVARIANT_CHECK_INTERVAL:-0}

LOG_DIR="build/generated/logs"
mkdir -p $LOG_DIR

echo "Starting the seid process for node $NODE_ID with invariant check interval=$INVARIANT_CHECK_INTERVAL..."

seid start --chain-id sei --inv-check-period ${INVARIANT_CHECK_INTERVAL} > "$LOG_DIR/seid-$NODE_ID.log" 2>&1 &
echo "Node $NODE_ID seid is started now"
echo "Done" >> build/generated/launch.complete

sleep 5

printf "12345678\n" | seid tx evm send-to-cast-sei-addr admin 500000000000000000000usei -y --fees=300000usei --broadcast-mode=block
