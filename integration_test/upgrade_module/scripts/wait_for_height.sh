#!/bin/bash

# Fetch the target block height from the parameters
TARGET_BLOCK_HEIGHT=${1//\'/}
TARGET_BLOCK_HEIGHT=${TARGET_BLOCK_HEIGHT//\"/}
PROGRESS_ACCOUNT=${2//\'/}
PROGRESS_ACCOUNT=${PROGRESS_ACCOUNT//\"/}

if [ -z "$TARGET_BLOCK_HEIGHT" ] || [ -z "$PROGRESS_ACCOUNT" ]; then
    echo "Usage: $0 <TARGET_BLOCK_HEIGHT> <PROGRESS_ACCOUNT>"
    exit 1
fi

# Fetch the node ID from the environment or default to 0
NODE_ID=${ID:-0}

# Keep this script bounded to avoid hanging CI jobs indefinitely.
STATUS_TIMEOUT_SECONDS=5
MAX_WAIT_SECONDS=360
START_TIME=$(date +%s)

PROGRESS_ADDR=$(printf "12345678\n" | seid keys show "$PROGRESS_ACCOUNT" -a 2>/dev/null)
if [ -z "$PROGRESS_ADDR" ]; then
   echo "Unable to resolve progress account $PROGRESS_ACCOUNT"
   exit 1
fi

# Loop until the target block height is reached or the service dies.
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
   if [[ "$CURRENT_BLOCK_HEIGHT" =~ ^[0-9]+$ ]]; then
      if [[ "$CURRENT_BLOCK_HEIGHT" -ge "$TARGET_BLOCK_HEIGHT" ]]; then
         echo "Block height reached at $CURRENT_BLOCK_HEIGHT"
         break
      fi

      echo "Waiting for block $TARGET_BLOCK_HEIGHT (current: $CURRENT_BLOCK_HEIGHT, elapsed: ${ELAPSED}s)"
   else
      echo "Seid status unavailable while waiting for block $TARGET_BLOCK_HEIGHT"
   fi
   printf "12345678\n" | seid tx bank send "$PROGRESS_ACCOUNT" "$PROGRESS_ADDR" 1usei \
      -y --chain-id sei --fees 2000usei --broadcast-mode sync >/dev/null 2>&1 || true
   sleep 0.5
done
