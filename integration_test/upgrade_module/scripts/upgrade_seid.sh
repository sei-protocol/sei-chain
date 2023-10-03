#!/bin/bash

NODE_ID=${ID:-0}
INVARIANT_CHECK_INTERVAL=${INVARIANT_CHECK_INTERVAL:-0}
LOG_DIR="build/generated/logs"

# appends version to the end of the existing list (env var)
NEW_LIST="$UPGRADE_VERSION_LIST,$1"

if [ -z "UPGRADE_VERSION_LIST" ]; then
    echo "Usage: $0 <UPGRADE_VERSION_LIST>"
    exit 1
fi

# kill the existing service
pkill -f "seid start"

# start the service with a different UPGRADE_VERSION_LIST
UPGRADE_VERSION_LIST=$NEW_LIST seid start --chain-id sei --inv-check-period ${INVARIANT_CHECK_INTERVAL} > "$LOG_DIR/seid-$NODE_ID.log" 2>&1 &

echo "PASS"

# sleep to give a chance to panic and log if needed
sleep 5
