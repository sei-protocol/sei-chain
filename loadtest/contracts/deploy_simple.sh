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

cd $seihome/loadtest/contracts/simpleexec && cargo build && docker run --rm -v "$(pwd)":/code \
  --mount type=volume,source="$(basename "$(pwd)")_cache",target=/code/target \
  --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry \
  cosmwasm/rust-optimizer:0.12.5

# Deploy all contracts
echo "Deploying contracts..."

cd $seihome/loadtest/contracts
# store
echo "Storing..."
storeres=$(printf "12345678\n" | $seidbin tx wasm store simpleexec/artifacts/simpleexec.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)

id=$(python3 parser.py code_id $storeres)

# instantiate
echo "Instantiating..."

valaddr=$(printf "12345678\n" | $seidbin keys show $(printf "12345678\n" | $seidbin keys show node_admin --output json | jq -r .address) --bech=val --output json | jq -r '.address')
printf "12345678\n" | $seidbin tx staking delegate $valaddr 1000000000usei --from=$keyname --chain-id=$chainid -b block -y

insres=$(printf "12345678\n" | $seidbin tx wasm instantiate $id '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
addr=$(python3 parser.py contract_address $insres)

sleep 90

printf "12345678\n" | $seidbin tx staking unbond $valaddr 1000000000usei --from=$keyname --chain-id=$chainid -b block -y

echo $addr
