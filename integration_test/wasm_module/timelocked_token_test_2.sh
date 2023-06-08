#!/bin/bash

# Make sure to deploy goblin and gringotts contract
CHAIN_ID=sei
seidbin=$(which ~/go/bin/seid | tr -d '"')
GRINGOTTS_ADDR=$(tail -1 integration_test/contracts/gringotts-contract-addr.txt |cut -d "," -f 1)

##########################################
# Proposal to propose_emergency_withdraw #
##########################################

unlocked=$($seidbin q wasm contract-state smart $GRINGOTTS_ADDR '{"info":{}}' --output json |jq -r .data.withdrawn_unlocked)
if [[ $unlocked -ne 0 ]]
then
  echo "Unlocked amount should be 0"
  exit 1
fi

# propose emergency withdrawal (withdraw all funds including locked to a designated address)
echo "Execute propose_emergency_withdraw for $GRINGOTTS_ADDR"
WITHDRWA_ADDR=$(printf "12345678\n" | $seidbin keys show admin1 -a)
RESULT=$(printf "12345678\n" | $seidbin tx wasm execute $GRINGOTTS_ADDR \
	'{"propose_emergency_withdraw":{"dst":"'$WITHDRWA_ADDR'"}}' \
	-y --from=admin1 \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block \
	--output json)
PROPOSAL_ID=$(echo $RESULT  | jq -r '.logs[].events[].attributes[] | select(.key == "proposal_id").value')
if [[ -z $PROPOSAL_ID ]]
then
  echo "Failed to propose update admin to $ADMIN_ADDR"
  exit 1
fi

printf "12345678\n" | $seidbin tx wasm execute $GRINGOTTS_ADDR \
	'{"vote_proposal":{"proposal_id":'$PROPOSAL_ID'}}' \
	-y --from=admin1 \
	--chain-id=$CHAIN_ID \
	 --gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

printf "12345678\n" | $seidbin tx wasm execute $GRINGOTTS_ADDR \
	'{"vote_proposal":{"proposal_id":'$PROPOSAL_ID'}}' \
	-y --from=admin2 \
	--chain-id=$CHAIN_ID \
	 --gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

printf "12345678\n" | $seidbin tx wasm execute $GRINGOTTS_ADDR \
	'{"vote_proposal":{"proposal_id":'$PROPOSAL_ID'}}' \
	-y --from=admin3 \
	--chain-id=$CHAIN_ID \
	 --gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

proposal_status=$($seidbin q wasm contract-state smart $GRINGOTTS_ADDR  '{"list_proposals":{}}' --output json |jq -r .data.proposals[0].status)
echo "Proposal status: $proposal_status"
if [ "$proposal_status" != "passed" ]
then
  echo "Proposal should be passed since we reach 75%"
  exit 1
fi

# once the proposal has 3/4 voting, an admin can execute the proposal
# with the following tx. Again it's same for both proposal types
printf "12345678\n" | $seidbin tx wasm execute $GRINGOTTS_ADDR \
	'{"process_proposal":{"proposal_id":'$PROPOSAL_ID'}}' \
	-y --from=admin3 \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

proposal_status=$($seidbin q wasm contract-state smart $GRINGOTTS_ADDR  '{"list_proposals":{}}' --output json |jq -r .data.proposals[0].status)
echo "Proposal status: $proposal_status"
if [ "$proposal_status" != "executed" ]
then
  echo "Proposal should be executed"
  exit 1
fi

# Verify account token
locked=$($seidbin q wasm contract-state smart $GRINGOTTS_ADDR '{"info":{}}' --output json |jq -r .data.withdrawn_locked)
if [[ $locked -eq 0 ]]
then
  echo "Locked amount should be greater than 0"
  exit 1
fi

echo "Test passed"