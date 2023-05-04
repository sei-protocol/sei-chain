#!/bin/bash
echo -n Admin Key Name:
read keyname
echo
echo -n Chain ID:
read chainid
echo
echo -n seid binary:
read seidbin
echo
echo -n sei-chain directory:
read seihome
echo

# Build all contracts
echo "Building contracts..."

cd $seihome/loadtest/contracts/jupiter && cargo build && docker run --rm -v "$(pwd)":/code \
  --mount type=volume,source="$(basename "$(pwd)")_cache",target=/code/target \
  --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry \
  cosmwasm/rust-optimizer:0.12.5

cd $seihome/loadtest/contracts/mars && cargo build && docker run --rm -v "$(pwd)":/code \
  --mount type=volume,source="$(basename "$(pwd)")_cache",target=/code/target \
  --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry \
  cosmwasm/rust-optimizer:0.12.5

cd $seihome/loadtest/contracts/saturn && cargo build && docker run --rm -v "$(pwd)":/code \
  --mount type=volume,source="$(basename "$(pwd)")_cache",target=/code/target \
  --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry \
  cosmwasm/rust-optimizer:0.12.5

cd $seihome/loadtest/contracts/venus && cargo build && docker run --rm -v "$(pwd)":/code \
  --mount type=volume,source="$(basename "$(pwd)")_cache",target=/code/target \
  --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry \
  cosmwasm/rust-optimizer:0.12.5

# Deploy all contracts
echo "Deploying contracts..."

cd $seihome/loadtest/contracts
# store
echo "Storing..."
jupiterstoreres=$(printf "12345678\n" | $seidbin tx wasm store jupiter/artifacts/jupiter.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
marsstoreres=$(printf "12345678\n" | $seidbin tx wasm store mars/artifacts/mars.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
saturnstoreres=$(printf "12345678\n" | $seidbin tx wasm store saturn/artifacts/saturn.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
venusstoreres=$(printf "12345678\n" | $seidbin tx wasm store venus/artifacts/venus.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
jupiterid=$(python3 parser.py code_id $jupiterstoreres)
marsid=$(python3 parser.py code_id $marsstoreres)
saturnid=$(python3 parser.py code_id $saturnstoreres)
venusid=$(python3 parser.py code_id $venusstoreres)

# instantiate
echo "Instantiating..."

valaddr=$(printf "12345678\n" | $seidbin keys show $(printf "12345678\n" | $seidbin keys show node_admin --output json | jq -r .address) --bech=val --output json | jq -r '.address')
printf "12345678\n" | $seidbin tx staking delegate $valaddr 1000000000usei --from=$keyname --chain-id=$chainid -b block -y

marsinsres=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr=$(python3 parser.py contract_address $marsinsres)
saturnaddr=$(python3 parser.py contract_address $saturninsres)
venusaddr=$(python3 parser.py contract_address $venusinsres)
jupiterinsres=$(printf "12345678\n" | $seidbin tx wasm instantiate $jupiterid '{"mars_address":"'$marsaddr'"}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
jupiteraddr=$(python3 parser.py contract_address $jupiterinsres)

# register
echo "Registering..."
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr $marsid false true  -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr $saturnid false true $marsaddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr $venusid false true -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $jupiteraddr $jupiterid false true $marsaddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"batch_contract_pair":[{"contract_addr":"'$jupiteraddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}]}' > jupiter.json
jupiterpair=$(printf "12345678\n" | $seidbin tx dex register-pairs jupiter.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}]}' > mars.json
marspair=$(printf "12345678\n" | $seidbin tx dex register-pairs mars.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}]}' > saturn.json
saturnpair=$(printf "12345678\n" | $seidbin tx dex register-pairs saturn.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}]}' > venus.json
venuspair=$(printf "12345678\n" | $seidbin tx dex register-pairs venus.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

sleep 90

printf "12345678\n" | $seidbin tx staking unbond $valaddr 1000000000usei --from=$keyname --chain-id=$chainid -b block -y

echo $jupiteraddr
echo $marsaddr
echo $saturnaddr
echo $venusaddr
