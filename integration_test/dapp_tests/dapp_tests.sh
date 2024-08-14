#!/bin/bash

# Check if a configuration argument is passed
if [ -z "$1" ]; then
  echo "Please provide a chain (seilocal, devnet, or testnet)."
  exit 1
fi

if [ -z "$2" ]; then
  echo "Please provide a mnemonic."
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
export DAPP_TESTS_MNEMONIC=$2
npx hardhat test --network $1 uniswap/uniswapTest.js
npx hardhat test --network $1 steak/SteakTests.js
