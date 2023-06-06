#!/bin/bash

# Make sure to deploy goblin and gringotts contract
CHAIN_ID=sei
seidbin=$(which ~/go/bin/seid | tr -d '"')
GRINGOTTS_ADDR=$(tail -1 integration_test/contracts/gringotts-contract-addr.txt |cut -d "," -f 1)

# Test for migration
GOBLIN_ADDR=$(tail -1 integration_test/contracts/goblin-contract-addr.txt |cut -d "," -f 1)
GRINGOTTS_ADDR=$(tail -1 integration_test/contracts/gringotts-contract-addr.txt |cut -d "," -f 1)
NEW_CODE_ID=$(tail -1 integration_test/contracts/gringotts-contract-addr.txt |cut -d "," -f 2)
RESULT=$(printf "12345678\n" | $seidbin tx wasm execute $GOBLIN_ADDR \
	'{"propose_migrate":{"contract_addr":"'$GRINGOTTS_ADDR'","new_code_id":'$NEW_CODE_ID',"msg":"e30="}}' \
	-y --from=admin1 \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block \
	--output=json)
PROPOSAL_ID=$(echo $RESULT  | jq -r '.logs[].events[].attributes[] | select(.key == "proposal_id").value')
if [[ -z $PROPOSAL_ID ]]
then
  echo "Failed to propose migration to code: $NEW_CODE_ID"
  exit 1
fi

printf "12345678\n" | $seidbin tx wasm execute $GOBLIN_ADDR \
	'{"vote_proposal":{"proposal_id":'$PROPOSAL_ID'}}' \
	-y --from=admin1 \
	--chain-id=$CHAIN_ID \
	 --gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

printf "12345678\n" | $seidbin tx wasm execute $GOBLIN_ADDR \
	'{"vote_proposal":{"proposal_id":'$PROPOSAL_ID'}}' \
	-y --from=admin2 \
	--chain-id=$CHAIN_ID \
	 --gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

printf "12345678\n" | $seidbin tx wasm execute $GOBLIN_ADDR \
	'{"vote_proposal":{"proposal_id":'$PROPOSAL_ID'}}' \
	-y --from=admin3 \
	--chain-id=$CHAIN_ID \
	 --gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block

proposal_status=$($seidbin q wasm contract-state smart $GOBLIN_ADDR  '{"list_proposals":{}}' --output json |jq -r .data.proposals[0].status)
echo "Proposal status: $proposal_status"
if [ "$proposal_status" != "passed" ]
then
  echo "Proposal should be passed since we reach 75%"
  exit 1
fi

printf "12345678\n" | $seidbin tx wasm execute $GOBLIN_ADDR \
	'{"process_proposal":{"proposal_id":'$PROPOSAL_ID'}}' \
	-y --from=admin3 \
	--chain-id=$CHAIN_ID \
	--gas=5000000 \
	--fees=1000000usei \
	--broadcast-mode=block
proposal_status=$($seidbin q wasm contract-state smart $GOBLIN_ADDR  '{"list_proposals":{}}' --output json |jq -r .data.proposals[0].status)
echo "Proposal status: $proposal_status"
if [ "$proposal_status" != "executed" ]
then
  echo "Proposal should be executed"
  exit 1
fi