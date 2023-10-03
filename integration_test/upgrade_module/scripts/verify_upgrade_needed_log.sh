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

echo "FAIL"
exit 1
