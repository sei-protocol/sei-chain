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
marsaddr4=$(python3 parser.py contract_address $marsinsres4)

# register
echo "Registering..."

valaddr=$(printf "12345678\n" | $seidbin keys show $(printf "12345678\n" | $seidbin keys show node_admin --output json | jq -r .address) --bech=val --output json | jq -r '.address')
printf "12345678\n" | $seidbin tx staking delegate $valaddr 1000000000usei --from=$keyname --chain-id=$chainid -b block -y --fees 2000usei

printf "12345678\n" | $seidbin tx dex register-contract $marsaddr $marsid false true  -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr $saturnid false true -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr $venusid false true -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr2 $marsid false true  -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr2 $saturnid false true -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr2 $venusid false true -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr3 $marsid false true  -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $saturnaddr3 $saturnid false true -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $venusaddr3 $venusid false true -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $marsaddr4 $marsid false true  -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"title":"Mars","description":"mars","batch_contract_pair":[{"contract_addr":"'$marsaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}],"deposit":"1000000usei"}' > mars.json
marspair=$(printf "12345678\n" | $seidbin tx dex register-pairs-proposal mars.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
marsproposalid=$(python3 parser.py proposal_id $marspair)
printf "12345678\n" | $seidbin tx gov deposit $marsproposalid 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx gov vote $marsproposalid yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"title":"Saturn","description":"saturn","batch_contract_pair":[{"contract_addr":"'$saturnaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}],"deposit":"1000000usei"}' > saturn.json
saturnpair=$(printf "12345678\n" | $seidbin tx dex register-pairs-proposal saturn.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
saturnproposalid=$(python3 parser.py proposal_id $saturnpair)
printf "12345678\n" | $seidbin tx gov deposit $saturnproposalid 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx gov vote $saturnproposalid yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"title":"Venus","description":"venus","batch_contract_pair":[{"contract_addr":"'$venusaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}],"deposit":"1000000usei"}' > venus.json
venuspair=$(printf "12345678\n" | $seidbin tx dex register-pairs-proposal venus.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
venusproposalid=$(python3 parser.py proposal_id $venuspair)
printf "12345678\n" | $seidbin tx gov deposit $venusproposalid 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx gov vote $venusproposalid yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"title":"Mars2","description":"mars2","batch_contract_pair":[{"contract_addr":"'$marsaddr2'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}],"deposit":"1000000usei"}' > mars2.json
marspair2=$(printf "12345678\n" | $seidbin tx dex register-pairs-proposal mars2.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
marsproposalid2=$(python3 parser.py proposal_id $marspair2)
printf "12345678\n" | $seidbin tx gov deposit $marsproposalid2 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx gov vote $marsproposalid2 yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"title":"Saturn2","description":"saturn2","batch_contract_pair":[{"contract_addr":"'$saturnaddr2'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}],"deposit":"1000000usei"}' > saturn2.json
saturnpair2=$(printf "12345678\n" | $seidbin tx dex register-pairs-proposal saturn2.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
saturnproposalid2=$(python3 parser.py proposal_id $saturnpair2)
printf "12345678\n" | $seidbin tx gov deposit $saturnproposalid2 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx gov vote $saturnproposalid2 yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"title":"Venus2","description":"venus2","batch_contract_pair":[{"contract_addr":"'$venusaddr2'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}],"deposit":"1000000usei"}' > venus2.json
venuspair2=$(printf "12345678\n" | $seidbin tx dex register-pairs-proposal venus2.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
venusproposalid2=$(python3 parser.py proposal_id $venuspair2)
printf "12345678\n" | $seidbin tx gov deposit $venusproposalid2 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx gov vote $venusproposalid2 yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"title":"Mars3","description":"mars3","batch_contract_pair":[{"contract_addr":"'$marsaddr3'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}],"deposit":"1000000usei"}' > mars3.json
marspair3=$(printf "12345678\n" | $seidbin tx dex register-pairs-proposal mars3.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
marsproposalid3=$(python3 parser.py proposal_id $marspair3)
printf "12345678\n" | $seidbin tx gov deposit $marsproposalid3 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx gov vote $marsproposalid3 yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"title":"Saturn3","description":"saturn3","batch_contract_pair":[{"contract_addr":"'$saturnaddr3'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}],"deposit":"1000000usei"}' > saturn3.json
saturnpair3=$(printf "12345678\n" | $seidbin tx dex register-pairs-proposal saturn3.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
saturnproposalid3=$(python3 parser.py proposal_id $saturnpair3)
printf "12345678\n" | $seidbin tx gov deposit $saturnproposalid3 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx gov vote $saturnproposalid3 yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"title":"Venus3","description":"venus3","batch_contract_pair":[{"contract_addr":"'$venusaddr3'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}],"deposit":"1000000usei"}' > venus3.json
venuspair3=$(printf "12345678\n" | $seidbin tx dex register-pairs-proposal venus3.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
venusproposalid3=$(python3 parser.py proposal_id $venuspair3)
printf "12345678\n" | $seidbin tx gov deposit $venusproposalid3 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx gov vote $venusproposalid3 yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"title":"Mars4","description":"mars4","batch_contract_pair":[{"contract_addr":"'$marsaddr4'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}],"deposit":"1000000usei"}' > mars4.json
marspair4=$(printf "12345678\n" | $seidbin tx dex register-pairs-proposal mars4.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
marsproposalid4=$(python3 parser.py proposal_id $marspair4)
printf "12345678\n" | $seidbin tx gov deposit $marsproposalid4 10000000usei -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx gov vote $marsproposalid4 yes -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

sleep 90

printf "12345678\n" | $seidbin tx staking unbond $valaddr 1000000000usei --from=$keyname --chain-id=$chainid -b block -y --fees 2000usei

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
