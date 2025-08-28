#!/bin/bash
seidbin=$(which ~/go/bin/seid | tr -d '"')
keyname=$(printf "12345678\n" | $seidbin keys list --output json | jq ".[0].name" | tr -d '"')
chainid=$($seidbin status | jq ".NodeInfo.network" | tr -d '"')
seihome=$(git rev-parse --show-toplevel | tr -d '"')

echo $keyname
echo $seidbin
echo $chainid
echo $seihome

# Deploy all contracts
echo "Deploying sei tester contract"

cd $seihome/loadtest/contracts
# store
echo "Storing..."

sei_tester_res=$(printf "12345678\n" | $seidbin tx wasm store sei_tester.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
sei_tester_id=$(python3 parser.py code_id $sei_tester_res)

# instantiate
echo "Instantiating..."
tester_in_res=$(printf "12345678\n" | $seidbin tx wasm instantiate $sei_tester_id '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
tester_addr=$(python3 parser.py contract_address $tester_in_res)

# TODO fix once implemented in loadtest config
jq '.sei_tester_address = "'$tester_addr'"' $seihome/loadtest/config.json > $seihome/loadtest/config_temp.json && mv $seihome/loadtest/config_temp.json $seihome/loadtest/config.json


echo "Deployed contracts:"
echo $tester_addr

exit 0