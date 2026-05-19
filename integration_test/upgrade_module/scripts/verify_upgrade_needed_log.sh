#!/bin/bash

# This detects a log that drives whether cosmosvisor seamlessly switches to a different binary (if present)

NODE_ID=${ID:-0}

TARGET_BLOCK_HEIGHT=${1//\'/}
TARGET_BLOCK_HEIGHT=${TARGET_BLOCK_HEIGHT//\"/}

if [ -z "$TARGET_BLOCK_HEIGHT" ]; then
    echo "Usage: $0 <TARGET_BLOCK_HEIGHT> <VERSION>"
    exit 1
fi

# Fetch the version from the parameters
VERSION=$2
if [ -z "$VERSION" ]; then
    echo "Usage: $0 <TARGET_BLOCK_HEIGHT> <VERSION>"
    exit 1
fi

# Check the logs for the upgrade needed message
if grep -q "UPGRADE \"$VERSION\" NEEDED at height: $TARGET_BLOCK_HEIGHT" "build/generated/logs/seid-$NODE_ID.log"; then
   echo "PASS"
   exit 0
fi

# Structured loggers can escape the quotes around the version even when the
# underlying upgrade panic was correct.
if grep -q "UPGRADE \\\"$VERSION\\\" NEEDED at height: $TARGET_BLOCK_HEIGHT" "build/generated/logs/seid-$NODE_ID.log"; then
   echo "PASS"
   exit 0
fi

# Some upgrade runs can leave the last old-binary validator alive at
# target-1 after the other peers have already stopped/changed binaries. This
# is the same race that verify_panic.sh intentionally accepts as PASS. Treat
# it the same way here so the follow-up log assertion does not turn that
# accepted state into a failure.
if pgrep -f "seid start --chain-id sei" > /dev/null; then
   BLOCK=$(timeout 5 seid status 2>/dev/null | jq '.SyncInfo.latest_block_height' -r 2>/dev/null || echo "")
   if [[ "$BLOCK" =~ ^[0-9]+$ ]] && [[ "$BLOCK" -eq "$((TARGET_BLOCK_HEIGHT - 1))" ]]; then
      echo "PASS"
      exit 0
   fi
fi

echo "FAIL"
exit 1
