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

cd $seihome/loadtest/contracts/simpleexec && cargo build --release --target wasm32-unknown-unknown

# Deploy all contracts
echo "Deploying contracts..."

cd $seihome/loadtest/contracts
# store
echo "Storing..."
storeres=$(printf "12345678\n" | $seidbin tx wasm store simpleexec/target/wasm32-unknown-unknown/release/simpleexec.wasm -y --from=$keyname --chain-id=$chainid --gas=50000000 --fees=1000000usei --broadcast-mode=block --output=json)
echo $storeres
id=$(python3 parser.py code_id $storeres)

# instantiate
echo "Instantiating..."

insres=$(printf "12345678\n" | $seidbin tx wasm instantiate $id '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
echo $insres
addr=$(python3 parser.py contract_address $insres)

echo $addr
