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

# Source shared helpers (_get_max_proposal_id, find_proposal_by_title).
# Resolve path relative to this script so we work regardless of cwd.
seidbin=seid
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../../contracts/_tx_helpers.sh"

# Submit via -b sync (the cosmos KV indexer isn't fed under Autobahn,
# so -b block hangs to its 60s timeout). Identify the new proposal by
# scanning gov state for one whose title matches our submission.
MAX_BEFORE=$(_get_max_proposal_id)
# --chain-id is required for tx signing; -b block implicitly read it
# from the client config in some paths, -b sync surfaces "chain-id ()"
# in the signature-verify error if it's missing.
RESPONSE=$(printf "12345678\n" | seid tx gov submit-proposal software-upgrade $VERSION \
    --title $VERSION --from node_admin --chain-id sei --fees 2000usei -b sync -y \
    --upgrade-height=$TARGET_HEIGHT --description "test $TYPE release" \
    $UPGRADE_INFO_FLAG --is-expedited --deposit 20000000usei --output json)
CODE=$(echo "$RESPONSE" | jq -r '.code // 0')
if [ "$CODE" != "0" ]; then
    echo "proposal_submit CheckTx rejected: $(echo "$RESPONSE" | jq -r '.raw_log')" >&2
    exit 1
fi
TXHASH=$(echo "$RESPONSE" | jq -r '.txhash')

PROPOSAL_ID=$(find_proposal_by_title "$VERSION" "$MAX_BEFORE" "$TXHASH") || exit 1
echo $PROPOSAL_ID
