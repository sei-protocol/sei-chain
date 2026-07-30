#!/bin/bash

seidbin=$(which ~/go/bin/seid | tr -d '"')
keyname=admin
chainid=$($seidbin status | jq ".NodeInfo.network" | tr -d '"')
seihome=$(git rev-parse --show-toplevel | tr -d '"')
contract_name=$1
if [[ $# -ne 1 ]];
then
  echo "Need to provide a contract name (mars,saturn,venus)"
  exit 1
fi

source "$(dirname "$0")/../utils/_tx_helpers.sh"

# dex tx submitter mirroring bank_send_and_wait: -b sync + wait for the
# sender's sequence to advance. The dex register-* txs have no
# convenient single-query side effect, but a sequence advance is a
# causal "committed" signal independent of side effect.
keyaddress=$(printf "12345678\n" | $seidbin keys show "$keyname" -a 2>/dev/null)
dex_tx_and_wait() {
    local seq_before; seq_before=$(_get_account_sequence "$keyaddress")
    local resp; resp=$(printf "12345678\n" | "$@" --broadcast-mode=sync --output=json)
    local code; code=$(echo "$resp" | jq -r '.code // 0')
    if [ "$code" != "0" ]; then
        echo "dex_tx_and_wait CheckTx rejected: $(echo "$resp" | jq -r '.raw_log')" >&2
        return 1
    fi
    _wait_until "$keyaddress sequence > $seq_before" \
        "[ \$(_get_account_sequence $keyaddress) -gt $seq_before ]" || return 1
}

cd $seihome || exit
echo "Deploying $contract_name contract..."

echo "Storing contract..."
contract_id=$(store_wasm integration_test/contracts/"$contract_name".wasm) || exit 1
echo "Got contract id $contract_id"

echo "Instantiating contract..."
contract_addr=$(instantiate_wasm "$contract_id" '{}' dex --no-admin) || exit 1

# register
echo "Registering contract..."
dex_tx_and_wait $seidbin tx dex register-contract "$contract_addr" "$contract_id" false true 100000000000 \
    -y --from="$keyname" --chain-id="$chainid" --fees=100000000000usei --gas=500000 || exit 1

echo '{"batch_contract_pair":[{"contract_addr":"'$contract_addr'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > integration_test/contracts/"$contract_name"-pair.json
dex_tx_and_wait $seidbin tx dex register-pairs integration_test/contracts/"$contract_name"-pair.json \
    -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 || exit 1
rm -rf integration_test/contracts/"$contract_name"-pair.json

echo '{"batch_contract_pair":[{"contract_addr":"'$contract_addr'","pairs":[{"price_denom":"usei","asset_denom":"uatom","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > integration_test/contracts/"$contract_name"-pair.json
dex_tx_and_wait $seidbin tx dex register-pairs integration_test/contracts/"$contract_name"-pair.json \
    -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 || exit 1
rm -rf integration_test/contracts/"$contract_name"-pair.json

echo '{"batch_contract_pair":[{"contract_addr":"'$contract_addr'","pairs":[{"price_denom":"usei","asset_denom":"uatomatom","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > integration_test/contracts/"$contract_name"-pair.json
dex_tx_and_wait $seidbin tx dex register-pairs integration_test/contracts/"$contract_name"-pair.json \
    -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 || exit 1
rm -rf integration_test/contracts/"$contract_name"-pair.json

echo '{"batch_contract_pair":[{"contract_addr":"'$contract_addr'","pairs":[{"price_denom":"usei","asset_denom":"uatomatomatom","price_tick_size":"0.0000001", "quantity_tick_size":"0.0000001"}]}]}' > integration_test/contracts/"$contract_name"-pair.json
dex_tx_and_wait $seidbin tx dex register-pairs integration_test/contracts/"$contract_name"-pair.json \
    -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 || exit 1
rm -rf integration_test/contracts/"$contract_name"-pair.json


sleep 10

echo "Deployed contracts:"
echo "$contract_addr"
echo "$contract_addr" > $seihome/integration_test/contracts/"$contract_name"-addr.txt

exit 0
