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
jupiterstoreres=$($seidbin tx wasm store jupiter/artifacts/jupiter.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
marsstoreres=$($seidbin tx wasm store mars/artifacts/mars.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
saturnstoreres=$($seidbin tx wasm store saturn/artifacts/saturn.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
venusstoreres=$($seidbin tx wasm store venus/artifacts/venus.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
jupiterid=$(python3 parser.py code_id $jupiterstoreres)
marsid=$(python3 parser.py code_id $marsstoreres)
saturnid=$(python3 parser.py code_id $saturnstoreres)
venusid=$(python3 parser.py code_id $venusstoreres)

# instantiate
echo "Instantiating..."
marsinsres=$($seidbin tx wasm instantiate $marsid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
saturninsres=$($seidbin tx wasm instantiate $saturnid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
venusinsres=$($seidbin tx wasm instantiate $venusid '{}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
marsaddr=$(python3 parser.py contract_address $marsinsres)
saturnaddr=$(python3 parser.py contract_address $saturninsres)
venusaddr=$(python3 parser.py contract_address $venusinsres)
jupiterinsres=$($seidbin tx wasm instantiate $jupiterid '{"mars_address":"'$marsaddr'"}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
jupiteraddr=$(python3 parser.py contract_address $jupiterinsres)

# register
echo "Registering..."
$seidbin tx dex register-contract $marsaddr $marsid false true  -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
$seidbin tx dex register-contract $saturnaddr $saturnid false true $marsaddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
$seidbin tx dex register-contract $venusaddr $venusid false true -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
$seidbin tx dex register-contract $jupiteraddr $jupiterid false true $marsaddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"title":"Jupiter","description":"jupiter","batch_contract_pair":[{"contract_addr":"'$jupiteraddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}],"deposit":"1000000usei"}' > jupiter.json
jupiterpair=$($seidbin tx dex register-pairs-proposal jupiter.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
jupiterproposalid=$(python3 parser.py proposal_id $jupiterpair)
$seidbin tx gov deposit $jupiterproposalid 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
$seidbin tx gov vote $jupiterproposalid yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"title":"Mars","description":"mars","batch_contract_pair":[{"contract_addr":"'$marsaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}],"deposit":"1000000usei"}' > mars.json
marspair=$($seidbin tx dex register-pairs-proposal mars.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
marsproposalid=$(python3 parser.py proposal_id $marspair)
$seidbin tx gov deposit $marsproposalid 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
$seidbin tx gov vote $marsproposalid yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"title":"Saturn","description":"saturn","batch_contract_pair":[{"contract_addr":"'$saturnaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}],"deposit":"1000000usei"}' > saturn.json
saturnpair=$($seidbin tx dex register-pairs-proposal saturn.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
saturnproposalid=$(python3 parser.py proposal_id $saturnpair)
$seidbin tx gov deposit $saturnproposalid 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
$seidbin tx gov vote $saturnproposalid yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"title":"Venus","description":"venus","batch_contract_pair":[{"contract_addr":"'$venusaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}],"deposit":"1000000usei"}' > venus.json
venuspair=$($seidbin tx dex register-pairs-proposal venus.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
venusproposalid=$(python3 parser.py proposal_id $venuspair)
$seidbin tx gov deposit $venusproposalid 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
$seidbin tx gov vote $venusproposalid yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo "Jupiter address: "$jupiteraddr
echo "Mars address: "$marsaddr
echo "Saturn address: "$saturnaddr
echo "Venus address: "$venusaddr
