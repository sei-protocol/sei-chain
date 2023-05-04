#!/bin/bash
seidbin=$(which ~/go/bin/seid | tr -d '"')
keyname=$(printf "12345678\n" | $seidbin keys list --output json | jq ".[0].name" | tr -d '"')
chainid=$($seidbin status | jq ".NodeInfo.network" | tr -d '"')
seihome=$(git rev-parse --show-toplevel | tr -d '"')

echo $keyname
echo $seidbin
echo $chainid
echo $seihome

cd $seihome/loadtest/contracts/good && cargo build && docker run --rm -v "$(pwd)":/code \
  --mount type=volume,source="$(basename "$(pwd)")_cache",target=/code/target \
  --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry \
  cosmwasm/rust-optimizer:0.12.6

cd $seihome/loadtest/contracts/expensive && cargo build && docker run --rm -v "$(pwd)":/code \
  --mount type=volume,source="$(basename "$(pwd)")_cache",target=/code/target \
  --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry \
  cosmwasm/rust-optimizer:0.12.6

cd $seihome/loadtest/contracts/infinite && cargo build && docker run --rm -v "$(pwd)":/code \
  --mount type=volume,source="$(basename "$(pwd)")_cache",target=/code/target \
  --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry \
  cosmwasm/rust-optimizer:0.12.6

cd $seihome/loadtest/contracts/panic && cargo build && docker run --rm -v "$(pwd)":/code \
  --mount type=volume,source="$(basename "$(pwd)")_cache",target=/code/target \
  --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry \
  cosmwasm/rust-optimizer:0.12.6

echo "Deploying contracts..."

cd $seihome/loadtest/contracts
# store
echo "Storing..."
goodstoreres=$(printf "12345678\n" | $seidbin tx wasm store good/artifacts/good.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
expensivestoreres=$(printf "12345678\n" | $seidbin tx wasm store expensive/artifacts/expensive.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
infinitestoreres=$(printf "12345678\n" | $seidbin tx wasm store infinite/artifacts/infinite.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
panicstoreres=$(printf "12345678\n" | $seidbin tx wasm store panic/artifacts/panic.wasm -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
goodid=$(python3 parser.py code_id $goodstoreres)
expensiveid=$(python3 parser.py code_id $expensivestoreres)
infiniteid=$(python3 parser.py code_id $infinitestoreres)
panicid=$(python3 parser.py code_id $panicstoreres)

### Scenario: good A -> good B (happy path)
echo "Setting up S1..."
s1bres=$(printf "12345678\n" | $seidbin tx wasm instantiate $goodid '{"downstreams":[]}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s1baddr=$(python3 parser.py contract_address $s1bres)
payload='{"downstreams":["'$s1baddr'"]}'
s1ares=$(printf "12345678\n" | $seidbin tx wasm instantiate $goodid $payload -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s1aaddr=$(python3 parser.py contract_address $s1ares)

printf "12345678\n" | $seidbin tx dex register-contract $s1baddr $goodid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $s1aaddr $goodid false true 100000000000 $s1baddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"batch_contract_pair":[{"contract_addr":"'$s1aaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s1apair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
echo '{"batch_contract_pair":[{"contract_addr":"'$s1baddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s1bpair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

### Scenario: good A -> good B, no dependency registration
echo "Setting up S2..."
s2bres=$(printf "12345678\n" | $seidbin tx wasm instantiate $goodid '{"downstreams":[]}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s2baddr=$(python3 parser.py contract_address $s2bres)
payload='{"downstreams":["'$s2baddr'"]}'
s2ares=$(printf "12345678\n" | $seidbin tx wasm instantiate $goodid $payload -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s2aaddr=$(python3 parser.py contract_address $s2ares)

printf "12345678\n" | $seidbin tx dex register-contract $s2baddr $goodid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $s2aaddr $goodid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"batch_contract_pair":[{"contract_addr":"'$s2aaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s2apair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
echo '{"batch_contract_pair":[{"contract_addr":"'$s2baddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s2bpair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

### Scenario: expensive -> good
echo "Setting up S3..."
s3bres=$(printf "12345678\n" | $seidbin tx wasm instantiate $goodid '{"downstreams":[]}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s3baddr=$(python3 parser.py contract_address $s3bres)
payload='{"downstreams":["'$s3baddr'"]}'
s3ares=$(printf "12345678\n" | $seidbin tx wasm instantiate $expensiveid $payload -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s3aaddr=$(python3 parser.py contract_address $s3ares)

printf "12345678\n" | $seidbin tx dex register-contract $s3baddr $goodid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $s3aaddr $expensiveid false true 100000000000 $s3baddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"batch_contract_pair":[{"contract_addr":"'$s3aaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s3apair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
echo '{"batch_contract_pair":[{"contract_addr":"'$s3baddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s3bpair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

### Scenario: panic -> good
echo "Setting up S4..."
s4bres=$(printf "12345678\n" | $seidbin tx wasm instantiate $goodid '{"downstreams":[]}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s4baddr=$(python3 parser.py contract_address $s4bres)
payload='{"downstreams":["'$s4baddr'"]}'
s4ares=$(printf "12345678\n" | $seidbin tx wasm instantiate $panicid $payload -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s4aaddr=$(python3 parser.py contract_address $s4ares)

printf "12345678\n" | $seidbin tx dex register-contract $s4baddr $goodid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $s4aaddr $panicid false true 100000000000 $s4baddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"batch_contract_pair":[{"contract_addr":"'$s4aaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s4apair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
echo '{"batch_contract_pair":[{"contract_addr":"'$s4baddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s4bpair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

### Scenario: infinite -> good
echo "Setting up S5..."
s5bres=$(printf "12345678\n" | $seidbin tx wasm instantiate $goodid '{"downstreams":[]}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s5baddr=$(python3 parser.py contract_address $s5bres)
payload='{"downstreams":["'$s5baddr'"]}'
s5ares=$(printf "12345678\n" | $seidbin tx wasm instantiate $infiniteid $payload -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s5aaddr=$(python3 parser.py contract_address $s5ares)

printf "12345678\n" | $seidbin tx dex register-contract $s5baddr $goodid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $s5aaddr $infiniteid false true 100000000000 $s5baddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"batch_contract_pair":[{"contract_addr":"'$s5aaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s5apair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
echo '{"batch_contract_pair":[{"contract_addr":"'$s5baddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s5bpair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

### Scenario: good -> expensive
echo "Setting up S6..."
s6bres=$(printf "12345678\n" | $seidbin tx wasm instantiate $expensiveid '{"downstreams":[]}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s6baddr=$(python3 parser.py contract_address $s6bres)
payload='{"downstreams":["'$s6baddr'"]}'
s6ares=$(printf "12345678\n" | $seidbin tx wasm instantiate $goodid $payload -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s6aaddr=$(python3 parser.py contract_address $s6ares)

printf "12345678\n" | $seidbin tx dex register-contract $s6baddr $expensiveid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $s6aaddr $goodid false true 100000000000 $s6baddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"batch_contract_pair":[{"contract_addr":"'$s6aaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s6apair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
echo '{"batch_contract_pair":[{"contract_addr":"'$s6baddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s6bpair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

### Scenario: good -> panic
echo "Setting up S7..."
s7bres=$(printf "12345678\n" | $seidbin tx wasm instantiate $panicid '{"downstreams":[]}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s7baddr=$(python3 parser.py contract_address $s7bres)
payload='{"downstreams":["'$s7baddr'"]}'
s7ares=$(printf "12345678\n" | $seidbin tx wasm instantiate $goodid $payload -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s7aaddr=$(python3 parser.py contract_address $s7ares)

printf "12345678\n" | $seidbin tx dex register-contract $s7baddr $panicid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $s7aaddr $goodid false true 100000000000 $s7baddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"batch_contract_pair":[{"contract_addr":"'$s7aaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s7apair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
echo '{"batch_contract_pair":[{"contract_addr":"'$s7baddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s7bpair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

### Scenario: good -> infinite
echo "Setting up S8..."
s8bres=$(printf "12345678\n" | $seidbin tx wasm instantiate $panicid '{"downstreams":[]}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s8baddr=$(python3 parser.py contract_address $s8bres)
payload='{"downstreams":["'$s8baddr'"]}'
s8ares=$(printf "12345678\n" | $seidbin tx wasm instantiate $infiniteid $payload -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s8aaddr=$(python3 parser.py contract_address $s8ares)

printf "12345678\n" | $seidbin tx dex register-contract $s8baddr $infiniteid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $s8aaddr $goodid false true 100000000000 $s8baddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"batch_contract_pair":[{"contract_addr":"'$s8aaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s8apair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
echo '{"batch_contract_pair":[{"contract_addr":"'$s8baddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s8bpair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

### Scenario: panic A -> panic B
echo "Setting up S9..."
s9bres=$(printf "12345678\n" | $seidbin tx wasm instantiate $panicid '{"downstreams":[]}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s9baddr=$(python3 parser.py contract_address $s9bres)
payload='{"downstreams":["'$s9baddr'"]}'
s9ares=$(printf "12345678\n" | $seidbin tx wasm instantiate $panicid $payload -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s9aaddr=$(python3 parser.py contract_address $s9ares)

printf "12345678\n" | $seidbin tx dex register-contract $s9baddr $panicid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $s9aaddr $panicid false true 100000000000 $s9baddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"batch_contract_pair":[{"contract_addr":"'$s9aaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s9apair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
echo '{"batch_contract_pair":[{"contract_addr":"'$s9baddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s9bpair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

### Scenario: infinite A -> infinite B
echo "Setting up S10..."
s10bres=$(printf "12345678\n" | $seidbin tx wasm instantiate $infiniteid '{"downstreams":[]}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s10baddr=$(python3 parser.py contract_address $s10bres)
payload='{"downstreams":["'$s10baddr'"]}'
s10ares=$(printf "12345678\n" | $seidbin tx wasm instantiate $infiniteid $payload -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s10aaddr=$(python3 parser.py contract_address $s10ares)

printf "12345678\n" | $seidbin tx dex register-contract $s10baddr $infiniteid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $s10aaddr $infiniteid false true 100000000000 $s10baddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"batch_contract_pair":[{"contract_addr":"'$s10aaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s10apair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
echo '{"batch_contract_pair":[{"contract_addr":"'$s10baddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s10bpair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

### Scenario: infinite -> good, expensive -> good, panic -> good
echo "Setting up S12..."
s12dres=$(printf "12345678\n" | $seidbin tx wasm instantiate $goodid '{"downstreams":[]}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s12daddr=$(python3 parser.py contract_address $s12dres)
payload='{"downstreams":["'$s12daddr'"]}'
s12ares=$(printf "12345678\n" | $seidbin tx wasm instantiate $expensiveid $payload -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s12aaddr=$(python3 parser.py contract_address $s12ares)
s12bres=$(printf "12345678\n" | $seidbin tx wasm instantiate $panicid $payload -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s12baddr=$(python3 parser.py contract_address $s12bres)
s12cres=$(printf "12345678\n" | $seidbin tx wasm instantiate $infiniteid $payload -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s12caddr=$(python3 parser.py contract_address $s12cres)

printf "12345678\n" | $seidbin tx dex register-contract $s12daddr $goodid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $s12aaddr $expensiveid false true 100000000000 $s12daddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $s12baddr $panicid false true 100000000000 $s12daddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $s12caddr $infiniteid false true 100000000000 $s12daddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"batch_contract_pair":[{"contract_addr":"'$s12aaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s12apair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
echo '{"batch_contract_pair":[{"contract_addr":"'$s12baddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s12bpair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
echo '{"batch_contract_pair":[{"contract_addr":"'$s12caddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s12cpair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
echo '{"batch_contract_pair":[{"contract_addr":"'$s12daddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s12dpair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

### Scenario: good A -> good B -> good A (no dependency registration from B to A since it's not possible due to validation)
echo "Setting up S13..."
s13bres=$(printf "12345678\n" | $seidbin tx wasm instantiate $goodid '{"downstreams":[]}' -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block  --label=dex --output=json)
s13baddr=$(python3 parser.py contract_address $s13bres)
payload='{"downstreams":["'$s13baddr'"]}'
s13ares=$(printf "12345678\n" | $seidbin tx wasm instantiate $goodid $payload -y --no-admin --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --label=dex --output=json)
s13aaddr=$(python3 parser.py contract_address $s13ares)
payload='{"update_downstreams":{"downstreams":["'$s13aaddr'"]}}'
printf "12345678\n" | $seidbin tx wasm execute $s13baddr $payload -y --from=$keyname --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json

printf "12345678\n" | $seidbin tx dex register-contract $s13baddr $goodid false true 100000000000 -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block
printf "12345678\n" | $seidbin tx dex register-contract $s13aaddr $goodid false true 100000000000 $s13baddr -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block

echo '{"batch_contract_pair":[{"contract_addr":"'$s13aaddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s13apair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)
echo '{"batch_contract_pair":[{"contract_addr":"'$s13baddr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > pair.json
s13bpair=$(printf "12345678\n" | $seidbin tx dex register-pairs pair.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json)

sleep 90

jq '.contract_distribution = [{"contract_address": "'$s1baddr'", percentage: "0.04"}]' $seihome/loadtest/config.json > $seihome/loadtest/config_temp.json && mv $seihome/loadtest/config_temp.json $seihome/loadtest/config.json

for addr in $s2aaddr $s2baddr $s3aaddr $s3baddr $s4aaddr $s4baddr $s5aaddr $s5baddr $s6aaddr $s6baddr $s7aaddr $s7baddr $s8aaddr $s8baddr $s9aaddr $s9baddr $s10aaddr $s10baddr $s12aaddr $s12baddr $s12caddr $s12daddr $s13aaddr $s13baddr
do
  jq '.contract_distribution += [{"contract_address": "'$addr'", percentage: "0.04"}]' $seihome/loadtest/config.json > $seihome/loadtest/config_temp.json && mv $seihome/loadtest/config_temp.json $seihome/loadtest/config.json
done

echo "Deployed contracts:"
echo $s1baddr
echo $s2aaddr
echo $s2baddr
echo $s3aaddr
echo $s3baddr
echo $s4aaddr
echo $s4baddr
echo $s5aaddr
echo $s5baddr
echo $s6aaddr
echo $s6baddr
echo $s7aaddr
echo $s7baddr
echo $s8aaddr
echo $s8baddr
echo $s9aaddr
echo $s9baddr
echo $s10aaddr
echo $s10baddr
echo $s12aaddr
echo $s12baddr
echo $s12caddr
echo $s12daddr
echo $s13aaddr
echo $s13baddr

exit 0
