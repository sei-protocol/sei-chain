#!/bin/bash

seidbin=$(which ~/go/bin/seid | tr -d '"')
keyname=$(printf "12345678\n" | $seidbin keys list --output json | jq ".[0].name" | tr -d '"')
chainid=$($seidbin status | jq ".NodeInfo.network" | tr -d '"')
seihome=$(git rev-parse --show-toplevel | tr -d '"')

# Deploy all contracts
echo "Deploying contracts..."

cd $seihome/loadtest/contracts
# store
echo "Storing contract..."
marsstoreres=$(printf "12345678\n" | $seidbin tx wasm store mars/artifacts/mars.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
marsid=$(python3 parser.py code_id $marsstoreres)

# instantiate
echo "Instantiating contract..."
marsinsres=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr=$(python3 parser.py contract_address $marsinsres)

# register
echo "Registering contract..."

printf "12345678\n" | $seidbin tx dex register-contract $marsaddr $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > mars.json
marspair=$(printf "12345678\n" | $seidbin tx dex register-pairs mars.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

sleep 30

echo "Deployed contracts:"
echo "$marsaddr"
echo "$marsaddr" > $seihome/loadtest/contracts/contract_output.txt

exit 0
