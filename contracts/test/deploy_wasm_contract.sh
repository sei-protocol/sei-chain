#!/bin/bash

seidbin=$(which ~/go/bin/seid | tr -d '"')
keyname=$(printf "12345678\n" | $seidbin keys list --output json | jq ".[0].name" | tr -d '"')
keyaddress=$(printf "12345678\n" | $seidbin keys list --output json | jq ".[0].address" | tr -d '"')
chainid=$($seidbin status | jq ".NodeInfo.network" | tr -d '"')
seihome=$(git rev-parse --show-toplevel | tr -d '"')

cd $seihome || exit
echo "Deploying wasm counter contract"

echo "Storing wasm counter contract"
store_result=$(printf "12345678\n" | $seidbin tx wasm store integration_test/contracts/counter_parallel.wasm -y --from="$keyname" --chain-id="$chainid" --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
contract_id=$(echo "$store_result" | jq -r '.logs[].events[].attributes[] | select(.key == "code_id").value')
echo "$contract_id" > contracts/wasm_code_id.txt
echo "Instantiating wasm counter contract"
instantiate_result=$(printf "12345678\n" | $seidbin tx wasm instantiate "$contract_id" '{"count": 0}' -y --no-admin --from="$keyname" --chain-id="$chainid" --gas=5000000 --fees=1000000usei --broadcast-mode=block --label=dex --output=json)
echo $instantiate_result

contract_addr=$(echo "$instantiate_result" |jq -r 'first(.logs[].events[].attributes[] | select(.key == "_contract_address").value)')

echo "Deployed counter contract with address:"
echo $contract_addr

#  write the contract address to wasm_contract_addr.txt
echo "$contract_addr" > contracts/wasm_contract_addr.txt