#!/bin/bash

# This script is used to deploy the UATOM ERC20 contract and associate it with the SEI account.
set -e

endpoint=${EVM_RPC:-"http://127.0.0.1:8545"}
owner1=0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52
associated_sei_account1=sei1m9qugvk4h66p6hunfajfg96ysc48zeq4m0d82c
owner2=0x70997970C51812dc3A010C7d01b50e0d17dc79C8

echo "Funding account $account with UATOM for testing..."
printf "12345678\n" | seid tx bank send $(printf "12345678\n" | seid keys show admin -a) $associated_sei_account1 10000uatom --fees 20000usei -b block -y

echo "Fund owners with some SEI"
printf "12345678\n" | seid tx evm send $owner1 1000000000000000000 --from admin
printf "12345678\n" | seid tx evm send $owner2 1000000000000000000 --from admin

echo "Deploying ERC20 pointer contract for UATOM..."
printf "12345678\n" | seid tx evm call-contract 0x000000000000000000000000000000000000100b c31d960f000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000057561746f6d000000000000000000000000000000000000000000000000000000 --from admin --evm-rpc=$endpoint
sleep 3
pointer_output=$(seid q evm pointer NATIVE uatom --output json)

erc20_deploy_addr=$(echo "$pointer_output" | jq .pointer | tr -d '"')
echo $erc20_deploy_addr > contracts/erc20_deploy_addr.txt

# wait for deployment to finish on live chain
sleep 3
