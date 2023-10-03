#!/bin/bash

# Fetch the node ID from the environment or default to 0
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

# Verify the service is NOT running
if pgrep -f "seid start --chain-id sei" > /dev/null; then
   echo "FAIL"
   exit 1
fi

# Check the logs for the panic message
PANIC_DETECTED=0
if grep -q "UPGRADE \"$VERSION\" NEEDED at height: $TARGET_BLOCK_HEIGHT" "build/generated/logs/seid-$NODE_ID.log"; then
   PANIC_DETECTED=1
   echo "PASS"
   exit 0
fi

echo "FAIL"
exit 1