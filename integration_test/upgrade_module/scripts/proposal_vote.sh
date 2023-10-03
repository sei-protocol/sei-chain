#!/bin/bash

PROPOSAL_ID=${1//\'/}  # Remove single quotes
PROPOSAL_ID=${PROPOSAL_ID//\"/}  # Remove double quotes

echo "PROPOSAL_ID=$PROPOSAL_ID" >> /tmp/proposal_id

printf "12345678\n" | seid tx gov vote $PROPOSAL_ID yes --from node_admin --chain-id sei --fees 2000usei -b block -y --output json | jq -r .code
