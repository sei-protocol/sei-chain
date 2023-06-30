#!/usr/bin/env sh

LOG_DIR="build/generated/logs"
mkdir -p $LOG_DIR

# Starting sei chain
echo "RPC Node is starting now, check logs under $LOG_DIR"

seid start --chain-id sei > "$LOG_DIR/rpc-node.log" 2>&1 &
echo "Done" >> build/generated/rpc-launch.complete