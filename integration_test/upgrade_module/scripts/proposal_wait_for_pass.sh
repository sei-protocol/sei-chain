#!/bin/bash

PROPOSAL_ID=$1
PROGRESS_ACCOUNT=${2//\'/}
PROGRESS_ACCOUNT=${PROGRESS_ACCOUNT//\"/}
TIMEOUT_SECONDS=300
INTERVAL_SECONDS=0.5
TRIES=600

if [ -z "$PROPOSAL_ID" ] || [ -z "$PROGRESS_ACCOUNT" ]; then
    echo "Usage: $0 <PROPOSAL_ID> <PROGRESS_ACCOUNT>"
    exit 1
fi

PROGRESS_ADDR=$(printf "12345678\n" | seid keys show "$PROGRESS_ACCOUNT" -a 2>/dev/null)
if [ -z "$PROGRESS_ADDR" ]; then
    echo "Unable to resolve progress account $PROGRESS_ACCOUNT"
    exit 1
fi

# Loop until the proposal status is PROPOSAL_STATUS_PASSED or we timeout
for ((i=1; i<=TRIES; i++)); do
    STATUS=$(seid query gov proposal $PROPOSAL_ID --output json | jq -r ".status")

    if [ "$STATUS" == "PROPOSAL_STATUS_PASSED" ]; then
        echo "Proposal $PROPOSAL_ID has passed!"
        exit 0
    else
        echo "Waiting for proposal $PROPOSAL_ID to pass... ($i/$TRIES)"
        printf "12345678\n" | seid tx bank send "$PROGRESS_ACCOUNT" "$PROGRESS_ADDR" 1usei \
            -y --chain-id sei --fees 2000usei --broadcast-mode sync >/dev/null 2>&1 || true
        sleep $INTERVAL_SECONDS
    fi
done

echo "Timeout reached. Exiting."
exit 1
