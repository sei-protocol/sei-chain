#!/usr/bin/env sh

LOG_DIR="build/generated/logs"
mkdir -p $LOG_DIR

# Starting sei chain
echo "Starting the sei chain rpc node"
seid start --chain-id sei > "$LOG_DIR/rpc-node.log" 2>&1 &
echo "RPC Node is started now, check logs under $LOG_DIR"