#!/bin/bash

TARGET_HEIGHT=$1
TYPE=$2

VERSION=$3
if [ -z "$VERSION" ]; then
    echo "Usage: $0 <TARGET_BLOCK_HEIGHT> <major | minor> <VERSION>"
    exit 1
fi

TARGET_HEIGHT=${1//\'/}  # Remove single quotes
TARGET_HEIGHT=${TARGET_HEIGHT//\"/}  # Remove double quotes

# Check the type and set INFO accordingly
if [ "$TYPE" == "major" ]; then
    INFO=""
    UPGRADE_INFO_FLAG=""
else
    INFO='{"upgradeType":"minor"}'
    UPGRADE_INFO_FLAG="--upgrade-info $INFO"
fi

PROPOSAL_ID=$(printf "12345678\n"  | seid tx gov submit-proposal software-upgrade $VERSION --title $VERSION --from node_admin --fees 2000usei -b block -y --upgrade-height=$TARGET_HEIGHT --description "test $TYPE release" $UPGRADE_INFO_FLAG --is-expedited --deposit 20000000usei --output json | jq -M -r ".logs[].events[].attributes[0] | select(.key == \"proposal_id\").value")

echo $PROPOSAL_ID
