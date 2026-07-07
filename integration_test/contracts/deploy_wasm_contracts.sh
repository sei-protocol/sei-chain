#!/bin/bash

# SEIDBIN / FIXTURE_SIGNER let a non-docker caller repoint the binary + signer
# without changing docker's behavior (both unset → the original computed values).
seidbin=${SEIDBIN:-$(which ~/go/bin/seid | tr -d '"')}
keyname=${FIXTURE_SIGNER:-$(printf "12345678\n" | $seidbin keys list --output json | jq ".[0].name" | tr -d '"')}
keyaddress=$(printf "12345678\n" | $seidbin keys show "$keyname" -a | tr -d '"')
chainid=$($seidbin status | jq ".NodeInfo.network" | tr -d '"')
seihome=$(git rev-parse --show-toplevel | tr -d '"')

source "$(dirname "$0")/../utils/_tx_helpers.sh"

cd $seihome || exit

# The suite's absolute code-ids (4/20) and counts (3/13/23/33) assume these are the
# only wasm codes after the 3 CW-pointer codes the EVM module's InitGenesis stores
# (ids 1-3). Fail loudly here if that isn't so — another wasm-storing suite ran first
# (e.g. `go test -shuffle`/-count, or a reorder), or the pointer set changed — rather
# than with a cryptic count mismatch later. Docker runs this first on a fresh cluster,
# so max == 3 there too.
max_code_id=$(_get_max_wasm_code_id)
if [ "$max_code_id" != "3" ]; then
    echo "seidb wasm fixture needs a pristine chain: max code id must be 3 (the baseline), got $max_code_id — another suite stored wasm first" >&2
    exit 1
fi

echo "Deploying first set of contracts..."

beginning_block_height=$($seidbin status | jq -r '.SyncInfo.latest_block_height')
echo "$beginning_block_height" > $seihome/integration_test/contracts/wasm_beginning_block_height.txt
echo "$keyaddress"  > $seihome/integration_test/contracts/wasm_creator_id.txt

# store first set of contracts
for i in {1..10}
do
    echo "Storing first set contract #$i..."
    contract_id=$(store_wasm integration_test/contracts/mars.wasm) || exit 1
    instantiate_wasm "$contract_id" '{}' dex --no-admin >/dev/null || exit 1
    echo "Got contract id $contract_id for iteration $i"
done

first_set_block_height=$($seidbin status | jq -r '.SyncInfo.latest_block_height')
echo "$first_set_block_height" > $seihome/integration_test/contracts/wasm_first_set_block_height.txt

sleep "${FIXTURE_SETTLE_SECONDS:-5}"

# store second set of contracts
for i in {11..20}
do
    echo "Storing second set contract #$i..."
    contract_id=$(store_wasm integration_test/contracts/saturn.wasm) || exit 1
    instantiate_wasm "$contract_id" '{}' dex --no-admin >/dev/null || exit 1
    echo "Got contract id $contract_id for iteration $i"
done

second_set_block_height=$($seidbin status | jq -r '.SyncInfo.latest_block_height')
echo "$second_set_block_height" > $seihome/integration_test/contracts/wasm_second_set_block_height.txt

sleep "${FIXTURE_SETTLE_SECONDS:-5}"

# store third set of contracts
for i in {21..30}
do
    echo "Storing third set contract #$i..."
    contract_id=$(store_wasm integration_test/contracts/venus.wasm) || exit 1
    instantiate_wasm "$contract_id" '{}' dex --no-admin >/dev/null || exit 1
    echo "Got contract id $contract_id for iteration $i"
done

third_set_block_height=$($seidbin status | jq -r '.SyncInfo.latest_block_height')
echo "$third_set_block_height" > $seihome/integration_test/contracts/wasm_third_set_block_height.txt

num_stored=$(seid q wasm list-code --count-total --limit 100 --output json | jq -r ".code_infos | length")
echo $num_stored

exit 0
