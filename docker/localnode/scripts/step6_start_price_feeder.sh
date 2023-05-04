#!/usr/bin/env sh

NODE_ID=${ID:-0}

LOG_DIR="build/generated/logs"
mkdir -p $LOG_DIR
ORACLE_CONFIG_FILE="build/generated/node_$NODE_ID/price_feeder_config.toml"

# Starting oracle price feeder
echo "Starting the oracle price feeder daemon"
printf "12345678\n" | price-feeder "$ORACLE_CONFIG_FILE" > "$LOG_DIR/price-feeder-$NODE_ID.log" 2>&1 &
echo "Node $NODE_ID started successfully! Check your logs under $LOG_DIR/"
