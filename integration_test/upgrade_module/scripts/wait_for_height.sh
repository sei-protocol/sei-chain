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
STATUS_TIMEOUT_SECONDS=${WAIT_FOR_HEIGHT_STATUS_TIMEOUT_SECONDS:-5}
MAX_WAIT_SECONDS=${WAIT_FOR_HEIGHT_MAX_WAIT_SECONDS:-180}
MAX_NO_PROGRESS_SECONDS=${WAIT_FOR_HEIGHT_MAX_NO_PROGRESS_SECONDS:-30}

# if other nodes die, this node can have no peers, and it will stay on block-1
# if that's the case, we should move forward (others are validating)
STUCK_COUNTER=0
NO_PROGRESS_COUNTER=0
LAST_BLOCK_HEIGHT=""
START_TIME=$(date +%s)

# Loop until the target block height is reached or the service dies, or it gets stuck on target-1 block
while true; do
   ELAPSED=$(( $(date +%s) - START_TIME ))
   if [[ "$ELAPSED" -ge "$MAX_WAIT_SECONDS" ]]; then
      echo "Timed out after ${MAX_WAIT_SECONDS}s waiting for block $TARGET_BLOCK_HEIGHT"
      exit 1
   fi

   # Check if the service is running (it might panic at the height and not let us reach it)
   if ! pgrep -f "seid start --chain-id sei" > /dev/null; then
      echo "Seid no longer running (panic)"
      break
   fi

   # Query the current block height of the node
   CURRENT_BLOCK_HEIGHT=$(timeout "$STATUS_TIMEOUT_SECONDS" seid status 2>/dev/null | jq '.SyncInfo.latest_block_height' -r 2>/dev/null)
   if ! [[ "$CURRENT_BLOCK_HEIGHT" =~ ^[0-9]+$ ]]; then
       ((NO_PROGRESS_COUNTER++))
       if [[ "$NO_PROGRESS_COUNTER" -ge "$MAX_NO_PROGRESS_SECONDS" ]]; then
           echo "Seid status unavailable for ${NO_PROGRESS_COUNTER}s while waiting for block $TARGET_BLOCK_HEIGHT"
           exit 1
       fi
       sleep 1
       continue
   fi

   if [[ -n "$LAST_BLOCK_HEIGHT" && "$CURRENT_BLOCK_HEIGHT" -le "$LAST_BLOCK_HEIGHT" ]]; then
       ((NO_PROGRESS_COUNTER++))
       if [[ "$NO_PROGRESS_COUNTER" -ge "$MAX_NO_PROGRESS_SECONDS" ]]; then
           echo "No block progress for ${NO_PROGRESS_COUNTER}s (current: $CURRENT_BLOCK_HEIGHT, target: $TARGET_BLOCK_HEIGHT)"
           exit 1
       fi
   else
       NO_PROGRESS_COUNTER=0
   fi
   LAST_BLOCK_HEIGHT="$CURRENT_BLOCK_HEIGHT"

   if [[ "$CURRENT_BLOCK_HEIGHT" -ge "$TARGET_BLOCK_HEIGHT" ]]; then
       echo "Block height reached at $CURRENT_BLOCK_HEIGHT"
       break
   fi

   if [[ "$CURRENT_BLOCK_HEIGHT" -eq "$((TARGET_BLOCK_HEIGHT - 1))" ]]; then
       ((STUCK_COUNTER++))
       if [[ $STUCK_COUNTER -ge 5 ]]; then
           echo "Exiting because stuck on block-1 (other peers panicked first)"
           exit 1
       fi
   else
       STUCK_COUNTER=0
   fi

   echo "Waiting for block $TARGET_BLOCK_HEIGHT (current: $CURRENT_BLOCK_HEIGHT, elapsed: ${ELAPSED}s)"

   sleep 1
done
