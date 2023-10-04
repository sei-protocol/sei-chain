#!/bin/bash

TARGET_BLOCK_HEIGHT=${1//\'/}
TARGET_BLOCK_HEIGHT=${TARGET_BLOCK_HEIGHT//\"/}

max_attempts=60
attempt=0

# Try for 1 minute to see if the service is NOT running
while [ $attempt -lt $max_attempts ]; do
    if pgrep -f "seid start --chain-id sei" > /dev/null; then
        BLOCK=$(seid status | jq '.SyncInfo.latest_block_height' -r)
        # If it's stuck on block-1, then that's okay (last one to panic can get stuck without peers)
        if [[ "$BLOCK" -eq "$((TARGET_BLOCK_HEIGHT - 1))" ]]; then
            echo "PASS"
            exit 0
        fi
        sleep 1  # wait for 1 second before checking again
        attempt=$((attempt+1))
    else
        echo "PASS"
        exit 0
    fi
done

echo "FAIL"
exit 1
