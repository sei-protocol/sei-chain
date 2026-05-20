#!/bin/bash

FROM_KEY=${1//\'/}  # Remove single quotes
FROM_KEY=${FROM_KEY//\"/}  # Remove double quotes
TO_ADDR=${2//\'/}
TO_ADDR=${TO_ADDR//\"/}
AMOUNT=${3//\'/}
AMOUNT=${AMOUNT//\"/}

if [ -z "$AMOUNT" ]; then
    echo "Usage: $0 <FROM_KEY> <TO_ADDR> <AMOUNT_WITH_DENOM>" >&2
    exit 1
fi

# Source shared helpers (_get_account_sequence, _wait_until,
# bank_send_and_get_height). Resolve path relative to this script so we
# work regardless of cwd.
seidbin=seid
chainid=sei
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../../contracts/_tx_helpers.sh"

bank_send_and_get_height "$FROM_KEY" "$TO_ADDR" "$AMOUNT" || exit 1
