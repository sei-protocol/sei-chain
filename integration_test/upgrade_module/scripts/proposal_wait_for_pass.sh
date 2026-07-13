#!/bin/bash

PROPOSAL_ID=$1
TIMEOUT=300
seidbin=seid
source integration_test/utils/_tx_helpers.sh

wait_for_proposal_status "$PROPOSAL_ID" "PROPOSAL_STATUS_PASSED" "admin" "$TIMEOUT" >/dev/null
echo "Proposal $PROPOSAL_ID has passed!"
