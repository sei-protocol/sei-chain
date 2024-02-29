#!/bin/bash

# This script is used to deploy the UATOM ERC20 contract and associate it with the SEI account.
set -e

endpoint=${EVM_RPC:-"http://127.0.0.1:8545"}
owner1=0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52
associated_sei_account1=sei1m9qugvk4h66p6hunfajfg96ysc48zeq4m0d82c
owner2=0x70997970C51812dc3A010C7d01b50e0d17dc79C8

shopt -s expand_aliases

echo "Funding account $account with UATOM for testing..."
seid tx bank send $(seid keys show admin -a) $associated_sei_account1 10000uatom --fees 20000usei -b block -y > /dev/null

echo "Fund owners with some SEI"
seid tx evm send $owner1 1000000000000000000 --from admin
seid tx evm send $owner2 1000000000000000000 --from admin

echo "Deploying ERC20 pointer contract for UATOM..."
deployment_output=$(seid tx evm deploy-erc20 uatom UATOM UATOM 6 --from admin --evm-rpc=$endpoint)

erc20_deploy_addr=$(echo "$deployment_output" | grep 'Deployed to:' | awk '{print $3}')
echo "ERC20_DEPLOY_ADDR=$erc20_deploy_addr"

# wait for deployment to finish on live chain
sleep 3
