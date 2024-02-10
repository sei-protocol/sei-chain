#!/bin/bash

cd loadtest/contracts/evm || exit 1

./setup.sh

/root/.foundry/bin/forge install OpenZeppelin/openzeppelin-contracts --no-commit &> /dev/null

/root/.foundry/bin/forge create -r http://127.0.0.1:8545 --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e src/NoopToken.sol:NoopToken --json --constructor-args "NoopToken" "NT" | jq -r '.deployedTo'
