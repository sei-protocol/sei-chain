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

# if other nodes die, this node can have no peers, and it will stay on block-1
# if that's the case, we should move forward (others are validating)
STUCK_COUNTER=0

# Loop until the target block height is reached or the service dies, or it gets stuck on target-1 block
while true; do
   # Check if the service is running (it might panic at the height and not let us reach it)
   if ! pgrep -f "seid start --chain-id sei" > /dev/null; then
      echo "Seid no longer running (panic)"
      break
   fi

   # Query the current block height of the node
   CURRENT_BLOCK_HEIGHT=$(seid status | jq '.SyncInfo.latest_block_height' -r)

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
   fi

   echo "Waiting for block $TARGET_BLOCK_HEIGHT (current: $CURRENT_BLOCK_HEIGHT)"

   sleep 1
done
