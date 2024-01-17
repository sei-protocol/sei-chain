#!/bin/bash

seidbin=$(which ~/go/bin/seid | tr -d '"')
keyname=$(printf "12345678\n" | $seidbin keys list --output json | jq ".[0].name" | tr -d '"')
chainid=$($seidbin status | jq ".NodeInfo.network" | tr -d '"')
seihome=$(git rev-parse --show-toplevel | tr -d '"')

cd $seihome || exit
echo "Deploying first set of contracts..."

beginning_block_height=$($seidbin status | jq -r '.SyncInfo.latest_block_height')
echo "$beginning_block_height" > $seihome/integration_test/contracts/wasm_beginning_block_height.txt
echo "$keyname"  > $seihome/integration_test/contracts/wasm_creator_id.txt

# store first set of contracts
for i in {1..100}
do
    echo "Storing first set contract #$i..."
    store_result=$(printf "12345678\n" | $seidbin tx wasm store integration_test/contracts/mars.wasm -y --from="$keyname" --chain-id="$chainid" --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
    contract_id=$(echo "$store_result" | jq -r '.logs[].events[].attributes[] | select(.key == "code_id").value')
    printf "12345678\n" | $seidbin tx wasm instantiate "$contract_id" '{}' -y --no-admin --from="$keyname" --chain-id="$chainid" --gas=5000000 --fees=1000000usei --broadcast-mode=block --label=dex --output=json
    echo "Got contract id $contract_id for iteration $i"
done

first_set_block_height=$($seidbin status | jq -r '.SyncInfo.latest_block_height')
echo "$first_set_block_height" > $seihome/integration_test/contracts/wasm_first_set_block_height.txt

forward_first_set=$($seidbin q wasm list-code --count-total --limit 400 --height $first_set_block_height --output json)
reverse_first_set=$($seidbin q wasm list-code --reverse --count-total --limit 400 --height $first_set_block_height --output json)

echo "100 forward first set $forward_first_set"
echo ""
echo "100 reverse first set $reverse_first_set"
echo ""

sleep 5s

# store second set of contracts
for i in {101..200}
do
    echo "Storing second set contract #$i..."
    store_result=$(printf "12345678\n" | $seidbin tx wasm store integration_test/contracts/saturn.wasm -y --from="$keyname" --chain-id="$chainid" --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
    contract_id=$(echo "$store_result" | jq -r '.logs[].events[].attributes[] | select(.key == "code_id").value')
    printf "12345678\n" | $seidbin tx wasm instantiate "$contract_id" '{}' -y --no-admin --from="$keyname" --chain-id="$chainid" --gas=5000000 --fees=1000000usei --broadcast-mode=block --label=dex --output=json
    echo "Got contract id $contract_id for iteration $i"
done

second_set_block_height=$($seidbin status | jq -r '.SyncInfo.latest_block_height')
echo "$second_set_block_height" > $seihome/integration_test/contracts/wasm_second_set_block_height.txt

forward_first_set=$($seidbin q wasm list-code --count-total --limit 400 --height $first_set_block_height --output json)
reverse_first_set=$($seidbin q wasm list-code --reverse --count-total --limit 400 --height $first_set_block_height --output json)

echo "200 forward first set $forward_first_set"
echo ""
echo "200 reverse first set $reverse_first_set"
echo ""

sleep 5s

# store third set of contracts
for i in {201..300}
do
    echo "Storing third set contract #$i..."
    store_result=$(printf "12345678\n" | $seidbin tx wasm store integration_test/contracts/venus.wasm -y --from="$keyname" --chain-id="$chainid" --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
    contract_id=$(echo "$store_result" | jq -r '.logs[].events[].attributes[] | select(.key == "code_id").value')
    printf "12345678\n" | $seidbin tx wasm instantiate "$contract_id" '{}' -y --no-admin --from="$keyname" --chain-id="$chainid" --gas=5000000 --fees=1000000usei --broadcast-mode=block --label=dex --output=json
    echo "Got contract id $contract_id for iteration $i"
done

third_set_block_height=$($seidbin status | jq -r '.SyncInfo.latest_block_height')
echo "$third_set_block_height" > $seihome/integration_test/contracts/wasm_third_set_block_height.txt

num_stored=$(seid q wasm list-code --count-total --limit 400 --output json | jq -r ".code_infos | length")
echo $num_stored

forward_first_set=$($seidbin q wasm list-code --count-total --limit 400 --height $first_set_block_height --output json)
reverse_first_set=$($seidbin q wasm list-code --reverse --count-total --limit 400 --height $first_set_block_height --output json)

echo "300 forward first set $forward_first_set"
echo ""
echo "300 reverse first set $reverse_first_set"
echo ""

exit 0
