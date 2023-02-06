#!/bin/bash
set -o errexit -o nounset -o pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
BASE_ACCOUNT=$(wasmd keys show validator -a)

echo "-----------------------"
echo "## Genesis CosmWasm contract"
wasmd add-wasm-genesis-message store "$DIR/../../x/wasm/keeper/testdata/hackatom.wasm" --instantiate-everybody true --builder=foo/bar:latest --run-as validator

echo "-----------------------"
echo "## Genesis CosmWasm instance"
INIT="{\"verifier\":\"$(wasmd keys show validator -a)\", \"beneficiary\":\"$(wasmd keys show fred -a)\"}"
wasmd add-wasm-genesis-message instantiate-contract 1 "$INIT" --run-as validator --label=foobar --amount=100ustake --admin "$BASE_ACCOUNT"

echo "-----------------------"
echo "## Genesis CosmWasm execute"
FIRST_CONTRACT_ADDR=wasm18vd8fpwxzck93qlwghaj6arh4p7c5n89k7fvsl
MSG='{"release":{}}'
wasmd add-wasm-genesis-message execute $FIRST_CONTRACT_ADDR "$MSG" --run-as validator --amount=1ustake

echo "-----------------------"
echo "## List Genesis CosmWasm codes"
wasmd add-wasm-genesis-message list-codes

echo "-----------------------"
echo "## List Genesis CosmWasm contracts"
wasmd add-wasm-genesis-message list-contracts
