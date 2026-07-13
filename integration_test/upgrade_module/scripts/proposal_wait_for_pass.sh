#!/bin/bash

PROPOSAL_ID=$1
TIMEOUT=300
seidbin=seid
chainid=sei
source integration_test/utils/_tx_helpers.sh

if wait_for_proposal_status "$PROPOSAL_ID" "PROPOSAL_STATUS_PASSED" "admin" "$TIMEOUT" >/dev/null; then
  echo "Proposal $PROPOSAL_ID has passed!"
else
  exit 1
fi
