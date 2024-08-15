#!/bin/bash

# Check if a configuration argument is passed
if [ -z "$1" ]; then
  echo "Please provide a chain (seilocal, devnet, or testnet)."
  exit 1
fi

set -e

# Build contacts repo first since we rely on that for lib.js
cd contracts
npm ci

cd ../integration_test/dapp_tests
npm ci

npx hardhat compile

# Set the CONFIG environment variable
export DAPP_TEST_ENV=$1

printf "$DAPP_TESTS_MNEMONIC" | seid keys add dapptest --recover --hd-path "m/44'/60'/0'/0/0" --keyring-backend test

npx hardhat test --network $1 uniswap/uniswapTest.js
npx hardhat test --network $1 steak/SteakTests.js
