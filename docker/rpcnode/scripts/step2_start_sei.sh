#!/usr/bin/env sh

LOG_DIR="build/generated/logs"
mkdir -p $LOG_DIR

# Starting sei chain
echo "Starting the sei chain rpc node"
seid start --chain-id sei