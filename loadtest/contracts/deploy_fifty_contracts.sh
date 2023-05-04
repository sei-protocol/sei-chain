#!/bin/bash
seidbin=$(which ~/go/bin/seid | tr -d '"')
keyname=$(printf "12345678\n" | $seidbin keys list --output json | jq ".[0].name" | tr -d '"')
chainid=$($seidbin status | jq ".NodeInfo.network" | tr -d '"')
seihome=$(git rev-parse --show-toplevel | tr -d '"')

echo $keyname
echo $seidbin
echo $chainid
echo $seihome

cd $seihome/loadtest/contracts/mars && cargo build && docker run --rm -v "$(pwd)":/code \
  --mount type=volume,source="$(basename "$(pwd)")_cache",target=/code/target \
  --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry \
  cosmwasm/rust-optimizer:0.12.6

cd $seihome/loadtest/contracts/saturn && cargo build && docker run --rm -v "$(pwd)":/code \
  --mount type=volume,source="$(basename "$(pwd)")_cache",target=/code/target \
  --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry \
  cosmwasm/rust-optimizer:0.12.6

cd $seihome/loadtest/contracts/venus && cargo build && docker run --rm -v "$(pwd)":/code \
  --mount type=volume,source="$(basename "$(pwd)")_cache",target=/code/target \
  --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry \
  cosmwasm/rust-optimizer:0.12.6

# Deploy all contracts
echo "Deploying contracts..."

cd $seihome/loadtest/contracts
# store
echo "Storing..."
marsstoreres=$(printf "12345678\n" | $seidbin tx wasm store mars/artifacts/mars.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
saturnstoreres=$(printf "12345678\n" | $seidbin tx wasm store saturn/artifacts/saturn.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
venusstoreres=$(printf "12345678\n" | $seidbin tx wasm store venus/artifacts/venus.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
marsid=$(python3 parser.py code_id $marsstoreres)
saturnid=$(python3 parser.py code_id $saturnstoreres)
venusid=$(python3 parser.py code_id $venusstoreres)

# instantiate
echo "Instantiating..."
marsinsres=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr=$(python3 parser.py contract_address $marsinsres)
saturnaddr=$(python3 parser.py contract_address $saturninsres)
venusaddr=$(python3 parser.py contract_address $venusinsres)

marsinsres2=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres2=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres2=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr2=$(python3 parser.py contract_address $marsinsres2)
saturnaddr2=$(python3 parser.py contract_address $saturninsres2)
venusaddr2=$(python3 parser.py contract_address $venusinsres2)

marsinsres3=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres3=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres3=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr3=$(python3 parser.py contract_address $marsinsres3)
saturnaddr3=$(python3 parser.py contract_address $saturninsres3)
venusaddr3=$(python3 parser.py contract_address $venusinsres3)

marsinsres4=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres4=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres4=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr4=$(python3 parser.py contract_address $marsinsres4)
saturnaddr4=$(python3 parser.py contract_address $saturninsres4)
venusaddr4=$(python3 parser.py contract_address $venusinsres4)

marsinsres5=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres5=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres5=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr5=$(python3 parser.py contract_address $marsinsres5)
saturnaddr5=$(python3 parser.py contract_address $saturninsres5)
venusaddr5=$(python3 parser.py contract_address $venusinsres5)

marsinsres6=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres6=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres6=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr6=$(python3 parser.py contract_address $marsinsres6)
saturnaddr6=$(python3 parser.py contract_address $saturninsres6)
venusaddr6=$(python3 parser.py contract_address $venusinsres6)

marsinsres7=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres7=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres7=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr7=$(python3 parser.py contract_address $marsinsres7)
saturnaddr7=$(python3 parser.py contract_address $saturninsres7)
venusaddr7=$(python3 parser.py contract_address $venusinsres7)

marsinsres8=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres8=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres8=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr8=$(python3 parser.py contract_address $marsinsres8)
saturnaddr8=$(python3 parser.py contract_address $saturninsres8)
venusaddr8=$(python3 parser.py contract_address $venusinsres8)

marsinsres9=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres9=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres9=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr9=$(python3 parser.py contract_address $marsinsres9)
saturnaddr9=$(python3 parser.py contract_address $saturninsres9)
venusaddr9=$(python3 parser.py contract_address $venusinsres9)

marsinsres10=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres10=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres10=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr10=$(python3 parser.py contract_address $marsinsres10)
saturnaddr10=$(python3 parser.py contract_address $saturninsres10)
venusaddr10=$(python3 parser.py contract_address $venusinsres10)

marsinsres11=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres11=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres11=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr11=$(python3 parser.py contract_address $marsinsres11)
saturnaddr11=$(python3 parser.py contract_address $saturninsres11)
venusaddr11=$(python3 parser.py contract_address $venusinsres11)

marsinsres12=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres12=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres12=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr12=$(python3 parser.py contract_address $marsinsres12)
saturnaddr12=$(python3 parser.py contract_address $saturninsres12)
venusaddr12=$(python3 parser.py contract_address $venusinsres12)

marsinsres13=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres13=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres13=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr13=$(python3 parser.py contract_address $marsinsres13)
saturnaddr13=$(python3 parser.py contract_address $saturninsres13)
venusaddr13=$(python3 parser.py contract_address $venusinsres13)

marsinsres14=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres14=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres14=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr14=$(python3 parser.py contract_address $marsinsres14)
saturnaddr14=$(python3 parser.py contract_address $saturninsres14)
venusaddr14=$(python3 parser.py contract_address $venusinsres14)

marsinsres15=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres15=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres15=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr15=$(python3 parser.py contract_address $marsinsres15)
saturnaddr15=$(python3 parser.py contract_address $saturninsres15)
venusaddr15=$(python3 parser.py contract_address $venusinsres15)

marsinsres16=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres16=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres16=$(printf "12345678\n" | $seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr16=$(python3 parser.py contract_address $marsinsres16)
saturnaddr16=$(python3 parser.py contract_address $saturninsres16)
venusaddr16=$(python3 parser.py contract_address $venusinsres16)

marsinsres17=$(printf "12345678\n" | $seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsress17=$(printf "12345678\n" | $seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr17=$(python3 parser.py contract_address $marsinsres17)
saturnaddr17=$(python3 parser.py contract_address $saturninsress17)

# register
echo "Registering..."

printf "12345678\n" | $seidbin tx dex register-contract $marsaddr $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr2 $marsid false true 1000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr2 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr2 $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr3 $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr3 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr3 $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr4 $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr4 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr4 $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr5 $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr5 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr5 $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr6 $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr6 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr6 $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr7 $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr7 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr7 $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr8 $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr8 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr8 $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr9 $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr9 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr9 $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr10 $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr10 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr10 $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr11 $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr11 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr11 $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr12 $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr12 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr12 $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr13 $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr13 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr13 $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr14 $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr14 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr14 $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr15 $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr15 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr15 $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr16 $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr16 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr16 $venusid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr17 $marsid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr17 $saturnid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=100000000000usei --gas=500000 --broadcast-mode=block


echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > mars.json
marspair=$(printf "12345678\n" | $seidbin tx dex register-pairs mars.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturn.json
saturnpair=$(printf "12345678\n" | $seidbin tx dex register-pairs saturn.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venus.json
venuspair=$(printf "12345678\n" | $seidbin tx dex register-pairs venus.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr2'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > mars2.json
marspair2=$(printf "12345678\n" | $seidbin tx dex register-pairs mars2.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr2'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturn2.json
saturnpair2=$(printf "12345678\n" | $seidbin tx dex register-pairs saturn2.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr2'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venus2.json
venuspair2=$(printf "12345678\n" | $seidbin tx dex register-pairs venus2.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr3'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > mars3.json
marspair3=$(printf "12345678\n" | $seidbin tx dex register-pairs mars3.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr3'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturn3.json
saturnpair3=$(printf "12345678\n" | $seidbin tx dex register-pairs saturn3.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr3'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venus3.json
venuspair3=$(printf "12345678\n" | $seidbin tx dex register-pairs venus3.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr4'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > mars4.json
marspair4=$(printf "12345678\n" | $seidbin tx dex register-pairs mars4.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr4'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturn4.json
saturnpair4=$(printf "12345678\n" | $seidbin tx dex register-pairs saturn4.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr4'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venus4.json
venuspair4=$(printf "12345678\n" | $seidbin tx dex register-pairs venus4.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr5'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > mars5.json
marspair5=$(printf "12345678\n" | $seidbin tx dex register-pairs mars5.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr5'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturn5.json
saturnpair5=$(printf "12345678\n" | $seidbin tx dex register-pairs saturn5.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr5'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venus5.json
venuspair5=$(printf "12345678\n" | $seidbin tx dex register-pairs venus5.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr6'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > mars6.json
marspair6=$(printf "12345678\n" | $seidbin tx dex register-pairs mars6.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr6'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturn6.json
saturnpair6=$(printf "12345678\n" | $seidbin tx dex register-pairs saturn6.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr6'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venus6.json
venuspair6=$(printf "12345678\n" | $seidbin tx dex register-pairs venus6.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr7'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > mars7.json
marspair7=$(printf "12345678\n" | $seidbin tx dex register-pairs mars7.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr7'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturn7.json
saturnpair7=$(printf "12345678\n" | $seidbin tx dex register-pairs saturn7.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr7'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venus7.json
venuspair7=$(printf "12345678\n" | $seidbin tx dex register-pairs venus7.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr8'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > mars8.json
marspair8=$(printf "12345678\n" | $seidbin tx dex register-pairs mars8.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr8'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturn8.json
saturnpair8=$(printf "12345678\n" | $seidbin tx dex register-pairs saturn8.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr8'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venus8.json
venuspair8=$(printf "12345678\n" | $seidbin tx dex register-pairs venus8.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr9'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > mars9.json
marspair9=$(printf "12345678\n" | $seidbin tx dex register-pairs mars9.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr9'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturn9.json
saturnpair9=$(printf "12345678\n" | $seidbin tx dex register-pairs saturn9.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr9'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venus9.json
venuspair9=$(printf "12345678\n" | $seidbin tx dex register-pairs venus9.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr10'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > mars10.json
marspair10=$(printf "12345678\n" | $seidbin tx dex register-pairs mars10.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr10'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturn10.json
saturnpair10=$(printf "12345678\n" | $seidbin tx dex register-pairs saturn10.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr10'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venus10.json
venuspair10=$(printf "12345678\n" | $seidbin tx dex register-pairs venus10.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr11'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > mars11.json
marspair11=$(printf "12345678\n" | $seidbin tx dex register-pairs mars11.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr11'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturn11.json
saturnpair11=$(printf "12345678\n" | $seidbin tx dex register-pairs saturn11.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr11'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venus11.json
venuspair11=$(printf "12345678\n" | $seidbin tx dex register-pairs venus11.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr12'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > mars12.json
marspair12=$(printf "12345678\n" | $seidbin tx dex register-pairs mars12.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr12'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturn12.json
saturnpair12=$(printf "12345678\n" | $seidbin tx dex register-pairs saturn12.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr12'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venus12.json
venuspair12=$(printf "12345678\n" | $seidbin tx dex register-pairs venus12.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr13'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > mars13.json
marspair13=$(printf "12345678\n" | $seidbin tx dex register-pairs mars13.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr13'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturn13.json
saturnpair13=$(printf "12345678\n" | $seidbin tx dex register-pairs saturn13.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr13'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venus13.json
venuspair13=$(printf "12345678\n" | $seidbin tx dex register-pairs venus13.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr14'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > marss14.json
marspairs14=$(printf "12345678\n" | $seidbin tx dex register-pairs marss14.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr14'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturns14.json
saturnpairs14=$(printf "12345678\n" | $seidbin tx dex register-pairs saturns14.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr14'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venuss14.json
venuspairs14=$(printf "12345678\n" | $seidbin tx dex register-pairs venuss14.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr15'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > marss15.json
marspairs15=$(printf "12345678\n" | $seidbin tx dex register-pairs marss15.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr15'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturns15.json
saturnpairs15=$(printf "12345678\n" | $seidbin tx dex register-pairs saturns15.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr15'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venuss15.json
venuspairs15=$(printf "12345678\n" | $seidbin tx dex register-pairs venuss15.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr16'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > marss16.json
marspairs16=$(printf "12345678\n" | $seidbin tx dex register-pairs marss16.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr16'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturns16.json
saturnpairs16=$(printf "12345678\n" | $seidbin tx dex register-pairs saturns16.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$venusaddr16'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > venuss16.json
venuspairs16=$(printf "12345678\n" | $seidbin tx dex register-pairs venuss16.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$marsaddr17'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > marss17.json
marspairs17=$(printf "12345678\n" | $seidbin tx dex register-pairs marss17.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

echo '{"batch_contract_pair":[{"contract_addr":"'$saturnaddr17'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > saturns17.json
saturnpairs17=$(printf "12345678\n" | $seidbin tx dex register-pairs saturns17.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

sleep 90

jq '.contract_distribution = [{"contract_address": "'$marsaddr'", percentage: "0.02"}]' $seihome/loadtest/config.json > $seihome/loadtest/config_temp.json && mv $seihome/loadtest/config_temp.json $seihome/loadtest/config.json

for addr in $saturnaddr $venusaddr $marsaddr2 $saturnaddr2 $venusaddr2 $marsaddr3 $saturnaddr3 $venusaddr3 $marsaddr4 $saturnaddr4 $venusaddr4 $marsaddr5 $saturnaddr5 $venusaddr5 $marsaddr6 $saturnaddr6 $venusaddr6 $marsaddr7 $saturnaddr7 $venusaddr7 $marsaddr8 $saturnaddr8 $venusaddr8 $marsaddr9 $saturnaddr9 $venusaddr9 $marsaddr10 $saturnaddr10 $venusaddr10 $marsaddr11 $saturnaddr11 $venusaddr11 $marsaddr12 $saturnaddr12 $venusaddr12 $marsaddr13 $saturnaddr13 $venusaddr13 $marsaddr14 $saturnaddr14 $venusaddr14 $marsaddr15 $saturnaddr15 $venusaddr15 $marsaddr16 $saturnaddr16 $venusaddr16 $marsaddr17 $saturnaddr17
do
  jq '.contract_distribution += [{"contract_address": "'$addr'", percentage: "0.02"}]' $seihome/loadtest/config.json > $seihome/loadtest/config_temp.json && mv $seihome/loadtest/config_temp.json $seihome/loadtest/config.json
done

echo "Deployed contracts:"
echo $marsaddr
echo $saturnaddr
echo $venusaddr
echo $marsaddr2
echo $saturnaddr2
echo $venusaddr2
echo $marsaddr3
echo $saturnaddr3
echo $venusaddr3
echo $marsaddr4
echo $saturnaddr4
echo $venusaddr4
echo $marsaddr5
echo $saturnaddr5
echo $venusaddr5
echo $marsaddr6
echo $saturnaddr6
echo $venusaddr6
echo $marsaddr7
echo $saturnaddr7
echo $venusaddr7
echo $marsaddr8
echo $saturnaddr8
echo $venusaddr8
echo $marsaddr9
echo $saturnaddr9
echo $venusaddr9
echo $marsaddr10
echo $saturnaddr10
echo $venusaddr10
echo $marsaddr11
echo $saturnaddr11
echo $venusaddr11
echo $marsaddr12
echo $saturnaddr12
echo $venusaddr12
echo $marsaddr13
echo $saturnaddr13
echo $venusaddr13
echo $marsaddr14
echo $saturnaddr14
echo $venusaddr14
echo $marsaddr15
echo $saturnaddr15
echo $venusaddr15
echo $marsaddr16
echo $saturnaddr16
echo $venusaddr16
echo $marsaddr17
echo $saturnaddr17


exit 0
