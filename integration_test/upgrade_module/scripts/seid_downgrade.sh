#!/bin/bash

# This downgrades the binary to the currently-set UPGRADE_VERSION_LIST
# UPGRADE_VERSION_LIST is an ENV var that is the default version for upgrade tests

NODE_ID=${ID:-0}
INVARIANT_CHECK_INTERVAL=${INVARIANT_CHECK_INTERVAL:-0}
LOG_DIR="build/generated/logs"
MAX_RESTART_ATTEMPTS=${SEID_RESTART_MAX_ATTEMPTS:-20}
RETRY_SLEEP_SECONDS=${SEID_RESTART_RETRY_SECONDS:-1}
PROCESS_EXIT_TIMEOUT_SECONDS=${SEID_PROCESS_EXIT_TIMEOUT_SECONDS:-30}
STARTUP_GRACE_SECONDS=${SEID_STARTUP_GRACE_SECONDS:-1}

# kill the existing service
pkill -f "seid start" || true

# Wait for the previous process to fully exit and release DB handles.
for ((i=0; i<PROCESS_EXIT_TIMEOUT_SECONDS; i++)); do
    if ! pgrep -f "seid start --chain-id sei" > /dev/null; then
        break
    fi
    sleep 1
done

if pgrep -f "seid start --chain-id sei" > /dev/null; then
    echo "FAIL"
    exit 1
fi

# Start the service with retries; startup can race DB lock release in CI.
for ((attempt=1; attempt<=MAX_RESTART_ATTEMPTS; attempt++)); do
    UPGRADE_VERSION_LIST=$UPGRADE_VERSION_LIST seid start --chain-id sei --inv-check-period ${INVARIANT_CHECK_INTERVAL} > "$LOG_DIR/seid-$NODE_ID.log" 2>&1 &
    sleep "$STARTUP_GRACE_SECONDS"

    if pgrep -f "seid start --chain-id sei" > /dev/null; then
        echo "PASS"
        exit 0
    fi

    sleep "$RETRY_SLEEP_SECONDS"
done

echo "FAIL"
exit 1
