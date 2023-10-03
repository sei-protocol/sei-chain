#!/bin/bash

PROPOSAL_ID=$1
TIMEOUT=60  # total wait time in seconds
INTERVAL=10  # time between checks in seconds
TRIES=$((TIMEOUT / INTERVAL))  # number of tries

# Loop until the proposal status is PROPOSAL_STATUS_PASSED or we timeout
for ((i=1; i<=TRIES; i++)); do
    STATUS=$(seid query gov proposal $PROPOSAL_ID --output json | jq -r ".status")

    if [ "$STATUS" == "PROPOSAL_STATUS_PASSED" ]; then
        echo "Proposal $PROPOSAL_ID has passed!"
        exit 0
    else
        echo "Waiting for proposal $PROPOSAL_ID to pass... ($i/$TRIES)"
        sleep $INTERVAL
    fi
done

echo "Timeout reached. Exiting."
exit 1
