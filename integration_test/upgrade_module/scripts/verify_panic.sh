#!/bin/bash

TARGET_BLOCK_HEIGHT=${1//\'/}
TARGET_BLOCK_HEIGHT=${TARGET_BLOCK_HEIGHT//\"/}

# Verify the service is NOT running
if pgrep -f "seid start --chain-id sei" > /dev/null; then
   BLOCK=$(seid status | jq '.SyncInfo.latest_block_height' -r)
   # If it's stuck on block-1, then that's okay (last one to panic can get stuck without peers)
   if [[ "$BLOCK" -eq "$((TARGET_BLOCK_HEIGHT - 1))" ]]; then
      echo "PASS"
      exit 0
   fi
   echo "FAIL"
   exit 1
fi

echo "PASS"
exit 0
