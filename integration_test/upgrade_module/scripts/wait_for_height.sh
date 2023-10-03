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

# Loop until the target block height is reached or the service dies
while true; do
   # Check if the service is running (it might panic at the height and not let us reach it)
   if ! pgrep -f "seid start --chain-id sei" > /dev/null; then
      echo "Service has stopped."
      break
   fi

   # Query the current block height of the node
   CURRENT_BLOCK_HEIGHT=$(seid status | jq '.SyncInfo.latest_block_height' -r)

   if [[ "$CURRENT_BLOCK_HEIGHT" -ge "$TARGET_BLOCK_HEIGHT" ]]; then
       echo "Reached target block height."
       break
   fi

   sleep 1
done
