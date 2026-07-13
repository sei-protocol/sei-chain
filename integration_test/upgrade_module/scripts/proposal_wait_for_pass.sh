#!/bin/bash

PROPOSAL_ID=$1
TIMEOUT=300  # total wait time in seconds
seidbin=seid
source integration_test/utils/_tx_helpers.sh

wait_for_proposal_status "$PROPOSAL_ID" "PROPOSAL_STATUS_PASSED" "$TIMEOUT" "admin" >/dev/null
echo "Proposal $PROPOSAL_ID has passed!"
