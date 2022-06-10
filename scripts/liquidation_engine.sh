#!/bin/bash -i
if [[ $# -eq 0 ]] ; then
    echo 'Usage: ./liquidation_engine.sh CREATOR_ACCOUNT_NAME'
    exit 0
fi

alias seid="./build/seid"
# TODO: use secret fetcher
keyring_passphrase='password'
contract_code=$(seid query wasm list-code | grep code_id | cut -d':' -f2 | tr -d '"')
contract_addresses=$(seid query wasm list-contracts $contract_code | grep '-' | cut -c 3-)
 
for contract_address in $contract_addresses; do
    # for each contract,
    # loop through all accounts and issue a liquidation request for each
    seid query auth accounts | grep address | while read -r line ; do
        account_address=$(echo "$line" | cut -d ":" -f 2 | tr -d ' ')
        nonce=$RANDOM
        yes $keyring_passphrase | seid tx dex liquidate $contract_address $nonce $account_address --chain-id=sei --from=$1 -y
    done
done