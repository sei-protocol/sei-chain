#!/bin/bash

# This script deploys the Disperse contract and outputs the deployed address only.
# Usage: ./deploy_disperse.sh <evm_rpc_url>

set -e

evm_endpoint=$1

if [[ -z "$evm_endpoint" ]]; then
  echo "EVM RPC endpoint required"
  exit 1
fi

# Ensure Foundry (forge/cast) & deps are installed *before* we use `cast`
cd loadtest/contracts/evm || exit 1

echo "Setting up Foundry..."
set +e
./setup.sh > /dev/null 2>&1
set -e
echo "Foundry setup complete"

# Ensure deployer account has funds (same hard-coded account used by other scripts)
THRESHOLD=100000000000000000000 # 100 ETH
ACCOUNT="0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52"
echo "Funding account $ACCOUNT with $THRESHOLD ETH"
BALANCE=$(cast balance $ACCOUNT --rpc-url "$evm_endpoint")
echo "Pre-funding account balance: $BALANCE"
if (( $(echo "$BALANCE < $THRESHOLD" | bc -l) )); then
  printf "12345678\n" | ~/go/bin/seid tx evm send $ACCOUNT 100000000000000000000 --from admin --evm-rpc "$evm_endpoint"
  sleep 3
  BALANCE=$(cast balance $ACCOUNT --rpc-url "$evm_endpoint")
  echo "Post-funding account balance: $BALANCE"
fi


git submodule update --init --recursive > /dev/null 2>&1

# Deploy Disperse (no constructor arguments)
/root/.foundry/bin/forge create --broadcast -r "$evm_endpoint" --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e src/Disperse.sol:Disperse --json | jq -r '.deployedTo' 