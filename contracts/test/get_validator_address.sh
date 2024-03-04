#!/bin/bash

# This script is used to find the validator address.
set -e

endpoint=${EVM_RPC:-"http://127.0.0.1:8545"}
owner1=0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52
associated_sei_account1=sei1m9qugvk4h66p6hunfajfg96ysc48zeq4m0d82c

validator_address=$(printf "12345678\n" | seid q staking validators -o json | jq -r '.validators[0].operator_address')

echo $validator_address > contracts/validator_address.txt
