#!/bin/bash

# This script is used to deploy the NoopToken contract to the target network
# This avoids trying to predict what address it might be deployed to

evm_endpoint=$1

cd loadtest/contracts/evm || exit 1

./setup.sh

git submodule update --init --recursive

/root/.foundry/bin/forge create -r "$evm_endpoint" --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e src/NoopToken.sol:NoopToken --json --constructor-args "NoopToken" "NT" | jq -r '.deployedTo'
