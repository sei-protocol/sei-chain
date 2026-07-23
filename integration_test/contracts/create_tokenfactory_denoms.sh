#!/bin/bash

seidbin=$(which ~/go/bin/seid | tr -d '"')
keyname=admin
keyaddress=$(printf "12345678\n" | $seidbin keys show "$keyname" -a 2>/dev/null)
chainid=$($seidbin status | jq ".NodeInfo.network" | tr -d '"')
seihome=$(git rev-parse --show-toplevel | tr -d '"')

source "$(dirname "$0")/../utils/_tx_helpers.sh"

cd $seihome || exit
echo "Deploying first set of tokenfactory denoms..."

# Capture a committed baseline height before the first denom creation. This
# script later records and compares state around "beginning block height", so
# under allow_empty_blocks=false we must force one real block and remember the
# exact inclusion height instead of sampling status at height 0 / an idle tip.
bootstrap_block_height=$(bank_send_and_get_height "$keyname" "$keyaddress" "1usei") || exit 1
beginning_block_height="$bootstrap_block_height"
echo "$beginning_block_height" > $seihome/integration_test/contracts/tfk_beginning_block_height.txt
echo "$keyaddress"  > $seihome/integration_test/contracts/tfk_creator_id.txt

# Create a tokenfactory denom and wait for it to appear on chain. The
# denom is deterministic: factory/<creator>/<subdenom>. Submitted via
# -b sync (the cosmos KV indexer isn't fed under Autobahn, so -b block
# hangs); poll denom-authority-metadata for non-empty admin as the
# side-effect signal that DeliverTx committed.
create_denom_and_wait() {
    local subdenom="$1"
    local new_token_denom="factory/$keyaddress/$subdenom"
    local resp; resp=$(printf "12345678\n" | $seidbin tx tokenfactory create-denom "$subdenom" \
        -y --from="$keyname" --chain-id="$chainid" --gas=500000 --fees=100000usei \
        --broadcast-mode=sync --output=json)
    local code; code=$(echo "$resp" | jq -r '.code // 0')
    if [ "$code" != "0" ]; then
        echo "create_denom_and_wait CheckTx rejected: $(echo "$resp" | jq -r '.raw_log')" >&2
        return 1
    fi
    # denom-authority-metadata returns {"authority_metadata":{"admin":""}}
    # (no error) for a non-existent denom; only a non-empty admin
    # indicates create-denom is committed.
    _wait_until "tokenfactory denom $new_token_denom" "
        admin=\$($seidbin q tokenfactory denom-authority-metadata $new_token_denom -o json 2>/dev/null | jq -r '.authority_metadata.admin // \"\"')
        [ -n \"\$admin\" ]
    " || return 1
    echo "$new_token_denom"
}

# create first set of tokenfactory denoms
for i in {1..10}
do
    echo "Creating first set of tokenfactory denoms #$i..."
    new_token_denom=$(create_denom_and_wait "$i") || exit 1
    echo "Got token $new_token_denom for iteration $i"
done


first_set_block_height=$($seidbin status | jq -r '.SyncInfo.latest_block_height')
echo "$first_set_block_height" > $seihome/integration_test/contracts/tfk_first_set_block_height.txt

sleep 5

# create second set of tokenfactory denoms
for i in {11..20}
do
    echo "Creating first set of tokenfactory denoms #$i..."
    new_token_denom=$(create_denom_and_wait "$i") || exit 1
    echo "Got token $new_token_denom for iteration $i"
done

second_set_block_height=$($seidbin status | jq -r '.SyncInfo.latest_block_height')
echo "$second_set_block_height" > $seihome/integration_test/contracts/tfk_second_set_block_height.txt

sleep 5

# create third set of tokenfactory denoms
for i in {21..30}
do
    echo "Creating first set of tokenfactory denoms #$i..."
    new_token_denom=$(create_denom_and_wait "$i") || exit 1
    echo "Got token $new_token_denom for iteration $i"
done

third_set_block_height=$($seidbin status | jq -r '.SyncInfo.latest_block_height')
echo "$third_set_block_height" > $seihome/integration_test/contracts/tfk_third_set_block_height.txt

num_denoms=$(seid q tokenfactory denoms-from-creator $CREATOR_ID --output json | jq -r ".denoms | length")
echo $num_denoms

exit 0
