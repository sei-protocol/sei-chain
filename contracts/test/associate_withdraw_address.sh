#!/bin/bash

# This script is used to deploy the UATOM ERC20 contract and associate it with the SEI account.
set -e

endpoint=${EVM_RPC:-"http://127.0.0.1:8545"}
owner1=0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52
associated_sei_account1=sei1m9qugvk4h66p6hunfajfg96ysc48zeq4m0d82c
owner2=0x70997970C51812dc3A010C7d01b50e0d17dc79C8

echo "Associating address"
~/go/bin/seid tx evm associate-address 0x57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e --from admin --evm-rpc=$endpoint

# wait for deployment to finish on live chain
sleep 3
