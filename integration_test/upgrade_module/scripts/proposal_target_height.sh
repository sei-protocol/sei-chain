#!/bin/bash

# Number of seconds passed as an argument
NUMBER_OF_SECONDS=$1

# Measure the actual block interval by sampling two heights a few seconds
# apart, instead of assuming a fixed value (the chain may run at ~300ms
# under CometBFT or ~400ms under Autobahn, and either can drift).
#
# This matters because the script returns
# `current_height + (seconds * 1000 / block_ms)`, used as the
# upgrade-height in a software-upgrade proposal. If block_ms is
# under-estimated, the target_height ends up too close to now: the
# voting period elapses, the proposal tallies, and ScheduleUpgrade
# rejects with "upgrade cannot be scheduled in the past".
SAMPLE_WINDOW_SECONDS=3

H1=$(seid status | jq -r '.SyncInfo.latest_block_height')
T1=$(date +%s%3N)
sleep "$SAMPLE_WINDOW_SECONDS"
H2=$(seid status | jq -r '.SyncInfo.latest_block_height')
T2=$(date +%s%3N)

DELTA_BLOCKS=$((H2 - H1))
DELTA_MS=$((T2 - T1))

if [ "$DELTA_BLOCKS" -gt 0 ] && [ "$DELTA_MS" -gt 0 ]; then
    BLOCK_MS=$((DELTA_MS / DELTA_BLOCKS))
else
    # Fallback if the sample produced no progress (chain stalled / restarting)
    BLOCK_MS=400
fi

NUMBER_OF_BLOCKS=$((NUMBER_OF_SECONDS * 1000 / BLOCK_MS))

HEIGHT=$((H2 + NUMBER_OF_BLOCKS))

echo $HEIGHT
