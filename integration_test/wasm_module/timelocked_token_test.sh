#!/bin/bash

# Make sure you have deployed goblin and gringotts contract
GRINGOTTS_ADDR=$1
CHAIN_ID=sei
VAL_ADDR=$(printf '12345678\n' | seid keys show node_admin --bech=val --output json | jq ".address")

###############
# Ops Actions #
###############
# delegate
seid tx wasm execute $GRINGOTTS_ADDR \
	'{"delegate":{"validator":"'$VAL_ADDR'","amount":"300000"}}' \
	-y --from=op \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block
# undelegate
seid tx wasm execute $GRINGOTTS_ADDR \
	'{"undelegate":{"validator":"'$VAL_ADDR'","amount":"300000"}}' \
	-y --from=op \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block
# redelegate TODO: find a way to get a different validator address


# withdraw vested tokens
# note that no partial withdrawal is supported, and op needs to make sure
# that enough staked tokens are already unbonded, otherwise the tx would fail
echo "Wait 15 seconds for tokens to be unbonded"
sleep 15
seid tx wasm execute $GRINGOTTS_ADDR \
	'{"initiate_withdraw_unlocked":{}}' \
	-y --from=op \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

# withdraw staking rewards
seid tx wasm execute $GRINGOTTS_ADDR \
	'{"initiate_withdraw_reward":{}}' \
	-y --from=op \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

#################
# Admin Actions #
#################

# add new op
OP_ADDR=<new op addr>
seid tx wasm execute $GRINGOTTS_ADDR \
	'{"update_op":{"op":"'$OP_ADDR'","remove":false}}' \
	-y --from=admin1 \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

# remove existing op
OP_ADDR=<existing op addr>
seid tx wasm execute $GRINGOTTS_ADDR \
	'{"update_op":{"op":"'$OP_ADDR'","remove":true}}' \
	-y --from=admin1 \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

# propose update admin (similar payload semantics as update op)
ADMIN_ADDR=<new/existing addr>
seid tx wasm execute $GRINGOTTS_ADDR \
	'{"propose_update_admin":{"admin":"'$ADMIN_ADDR'","remove":false}}' \
	-y --from=admin1 \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block
PROPOSAL_ID=<output from above>

# propose emergency withdrawal (withdraw all funds including locked to a
# designated address)
WITHDRWA_ADDR=<addr>
seid tx wasm execute $GRINGOTTS_ADDR \
	'{"propose_emergency_withdraw":{"dst":"'$ADMIN_ADDR'"}}' \
	-y --from=admin1 \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block
PROPOSAL_ID=<output from above>

# note that the proposer has already automatically voted for the proposal
# as part of the proposal tx, so they don't need to vote again here.
#
# vote logic is the same for both proposal types
seid tx wasm execute $GRINGOTTS_ADDR \
	'{"vote_proposal":{"proposal_id":'$PROPOSAL_ID'}}' \
	-y --from=g_admin2 \
	--chain-id=$CHAIN_ID \
	 --gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

# once the proposal has 3/4 voting, an admin can execute the proposal
# with the following tx. Again it's same for both proposal types
seid tx wasm execute $GRINGOTTS_ADDR \
	'{"process_proposal":{"proposal_id":'$PROPOSAL_ID'}}' \
	-y --from=g_admin3 \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

# TODO: Add test for migration

###########
# Queries #
###########
