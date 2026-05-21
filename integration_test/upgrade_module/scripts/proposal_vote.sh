#!/bin/bash

PROPOSAL_ID=${1//\'/}  # Remove single quotes
PROPOSAL_ID=${PROPOSAL_ID//\"/}  # Remove double quotes

echo "PROPOSAL_ID=$PROPOSAL_ID" >> /tmp/proposal_id

# Source shared helpers (_get_account_sequence, _wait_until). Resolve
# path relative to this script so we work regardless of cwd.
seidbin=seid
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../../utils/_tx_helpers.sh"

# Submit via -b sync and wait for the voter's sequence to advance —
# the upstream proposal-status polling tolerates a brief window between
# this script's return and the vote tally update, and the vote response
# itself isn't read by the yaml caller.
FROM=node_admin
FROM_ADDR=$(printf "12345678\n" | seid keys show "$FROM" -a 2>/dev/null)
SEQ_BEFORE=$(_get_account_sequence "$FROM_ADDR")
RESPONSE=$(printf "12345678\n" | seid tx gov vote $PROPOSAL_ID yes --from $FROM \
    --chain-id sei --fees 2000usei -b sync -y --output json)
CODE=$(echo "$RESPONSE" | jq -r '.code // 0')
if [ "$CODE" != "0" ]; then
    echo "proposal_vote CheckTx rejected: $(echo "$RESPONSE" | jq -r '.raw_log')" >&2
    echo "$CODE"
    exit 0
fi
_wait_until "$FROM_ADDR sequence > $SEQ_BEFORE" \
    "[ \$(_get_account_sequence $FROM_ADDR) -gt $SEQ_BEFORE ]" || exit 1
echo 0
