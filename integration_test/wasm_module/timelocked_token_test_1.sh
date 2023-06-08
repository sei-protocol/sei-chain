#!/bin/bash

# Make sure to deploy goblin and gringotts contract
CHAIN_ID=sei
seidbin=$(which ~/go/bin/seid | tr -d '"')
GRINGOTTS_ADDR=$(tail -1 integration_test/contracts/gringotts-contract-addr.txt |cut -d "," -f 1)
VAL_ADDR=$(printf '12345678\n' | $seidbin keys show node_admin --bech=val --output json | jq -r ".address")

###############
# Ops Actions #
###############
# delegate
echo "Delegating to validator $VAL_ADDR"
result=$(printf "12345678\n" | $seidbin tx wasm execute $GRINGOTTS_ADDR \
	'{"delegate":{"validator":"'$VAL_ADDR'","amount":"300000"}}' \
	-y --from=op \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block \
	--output=json)

# undelegate
echo "Undelegate from validator $VAL_ADDR"
result=$(printf "12345678\n" | $seidbin tx wasm execute $GRINGOTTS_ADDR \
	'{"undelegate":{"validator":"'$VAL_ADDR'","amount":"300000"}}' \
	-y --from=op \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block \
	--output=json)


echo "Wait 15 seconds for tokens to be unbonded"
sleep 15
unlocked=$($seidbin q wasm contract-state smart $GRINGOTTS_ADDR '{"info":{}}' --output json |jq -r .data.withdrawn_unlocked)
if [[ $unlocked -ne 0 ]]
then
  echo "Unlocked amount should be 0"
  exit 1
fi

# withdraw vested tokens
# note that no partial withdrawal is supported, and op needs to make sure
# that enough staked tokens are already unbonded, otherwise the tx would fail
echo "Execute initiate_withdraw_unlocked for $GRINGOTTS_ADDR"
printf "12345678\n" | $seidbin tx wasm execute $GRINGOTTS_ADDR \
	'{"initiate_withdraw_unlocked":{}}' \
	-y --from=op \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

# withdraw staking rewards
echo "Execute initiate_withdraw_reward for $GRINGOTTS_ADDR"
printf "12345678\n" | $seidbin tx wasm execute $GRINGOTTS_ADDR \
	'{"initiate_withdraw_reward":{}}' \
	-y --from=op \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

unlocked=$($seidbin q wasm contract-state smart $GRINGOTTS_ADDR '{"info":{}}' --output json |jq -r .data.withdrawn_unlocked)
if [[ $unlocked -eq 0 ]]
then
  echo "Unlocked amount should be greater than 0"
  exit 1
fi

#################
# Admin Actions #
#################
# add new op
echo "Execute update_op and add $OP_ADDR"
OP_ADDR=$(printf "12345678\n" | $seidbin keys show op -a)
printf "12345678\n" | $seidbin tx wasm execute $GRINGOTTS_ADDR \
	'{"update_op":{"op":"'$OP_ADDR'","remove":false}}' \
	-y --from=admin1 \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

# remove existing op
echo "Execute update_op and remove $OP_ADDR"
printf "12345678\n" | $seidbin tx wasm execute $GRINGOTTS_ADDR \
	'{"update_op":{"op":"'$OP_ADDR'","remove":true}}' \
	-y --from=admin1 \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

############################
# Proposal to update admin #
############################

# propose update admin (similar payload semantics as update op)
echo "Execute propose_update_admin and update $ADMIN_ADDR"
ADMIN_ADDR=$(printf "12345678\n" |$seidbin keys show admin4 -a)
RESULT=$(printf "12345678\n" | $seidbin tx wasm execute $GRINGOTTS_ADDR \
	'{"propose_update_admin":{"admin":"'$ADMIN_ADDR'","remove":false}}' \
	-y --from=admin1 \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block \
	--output json)
PROPOSAL_ID=$(echo $RESULT  | jq -r '.logs[].events[].attributes[] | select(.key == "proposal_id").value' )
if [[ -z $PROPOSAL_ID ]]
then
  echo "Failed to propose update admin to $ADMIN_ADDR"
  exit 1
fi

echo "Execute vote_proposal from admin1"
printf "12345678\n" | $seidbin tx wasm execute $GRINGOTTS_ADDR \
	'{"vote_proposal":{"proposal_id":'$PROPOSAL_ID'}}' \
	-y --from=admin1 \
	--chain-id=$CHAIN_ID \
	 --gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

echo "Execute vote_proposal from admin2"
printf "12345678\n" | $seidbin tx wasm execute $GRINGOTTS_ADDR \
	'{"vote_proposal":{"proposal_id":'$PROPOSAL_ID'}}' \
	-y --from=admin2 \
	--chain-id=$CHAIN_ID \
	 --gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

echo "Execute vote_proposal from admin3"
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
echo "Execute process_proposal"
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
  echo "Proposal should already be executed"
  exit 1
fi

echo "Test passed"