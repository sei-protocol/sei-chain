#!/bin/bash

# Fetch the target block height from the parameters
TARGET_BLOCK_HEIGHT=${1//\'/}
TARGET_BLOCK_HEIGHT=${TARGET_BLOCK_HEIGHT//\"/}

if [ -z "$TARGET_BLOCK_HEIGHT" ]; then
    echo "Usage: $0 <TARGET_BLOCK_HEIGHT>"
    exit 1
fi

# Fetch the node ID from the environment or default to 0
NODE_ID=${ID:-0}

# Keep this script bounded to avoid hanging CI jobs indefinitely.
MAX_WAIT_SECONDS=${WAIT_FOR_HEIGHT_MAX_WAIT_SECONDS:-360}
START_TIME=$(date +%s)
DEADLINE=$((START_TIME + MAX_WAIT_SECONDS))

# Loop until this node reaches the target block height.
while true; do
   NOW=$(date +%s)
   ELAPSED=$((NOW - START_TIME))
   REMAINING_SECONDS=$((DEADLINE - NOW))

   if [[ "$REMAINING_SECONDS" -le 0 ]]; then
      echo "Timed out after ${MAX_WAIT_SECONDS}s waiting for block $TARGET_BLOCK_HEIGHT"
      exit 1
   fi

   CURRENT_BLOCK_HEIGHT=$(timeout "$REMAINING_SECONDS" seid status 2>/dev/null | jq '.SyncInfo.latest_block_height' -r 2>/dev/null)
   if [[ "$CURRENT_BLOCK_HEIGHT" =~ ^[0-9]+$ ]]; then
      if [[ "$CURRENT_BLOCK_HEIGHT" -ge "$TARGET_BLOCK_HEIGHT" ]]; then
         echo "Block height reached at $CURRENT_BLOCK_HEIGHT"
         break
      fi

      echo "Waiting for block $TARGET_BLOCK_HEIGHT (current: $CURRENT_BLOCK_HEIGHT, elapsed: ${ELAPSED}s)"
   else
      echo "Seid status unavailable while waiting for block $TARGET_BLOCK_HEIGHT"
   fi
   sleep 1
done
