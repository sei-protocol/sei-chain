#!/bin/bash

# This script is used to deploy the NoopToken contract to the target network
# This avoids trying to predict what address it might be deployed to

evm_endpoint=$1

# first fund account if necessary
THRESHOLD=100000000000000000000 # 100 Eth
ACCOUNT="0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52"
BALANCE=$(cast balance $ACCOUNT --rpc-url "$evm_endpoint")
if (( $(echo "$BALANCE < $THRESHOLD" | bc -l) )); then
  printf "12345678\n" | ~/go/bin/seid tx evm send $ACCOUNT 100000000000000000000 --from admin --evm-rpc "$evm_endpoint"
  sleep 3
fi
cd loadtest/contracts/evm || exit 1

./setup.sh > /dev/null

git submodule update --init --recursive > /dev/null

/root/.foundry/bin/forge create -r "$evm_endpoint" --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e src/NoopToken.sol:NoopToken --json --constructor-args "NoopToken" "NT" | jq -r '.deployedTo'
