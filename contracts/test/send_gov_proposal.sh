#!/bin/bash

# This script is used to send a gov deposit.
set -e

endpoint=${EVM_RPC:-"http://127.0.0.1:8545"}
owner1=0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52
associated_sei_account1=sei1m9qugvk4h66p6hunfajfg96ysc48zeq4m0d82c

gov_proposal_output=$(printf "12345678\n" | seid tx gov submit-proposal param-change contracts/test/param_change_proposal.json --from admin --fees 20000usei -b block -y -o json | jq -r '.logs[0].events[3].attributes[1].value')

echo $gov_proposal_output > contracts/gov_proposal_output.txt
