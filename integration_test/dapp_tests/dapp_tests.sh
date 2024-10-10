#!/bin/bash

# Check if a configuration argument is passed
if [ -z "$1" ]; then
  echo "Please provide a chain (seilocal, devnet, or testnet)."
  exit 1
fi

IS_FAST_TRACK=false

# Check if -fast track enabled is present
for arg in "$@"; do
    if [[ "$arg" == "-f" ]]; then
        IS_FAST_TRACK=true
        break
    fi
done

export IS_FAST_TRACK

set -e

# Define the paths to the test files
uniswap_test="uniswap/uniswapTest.js"
steak_test="steak/SteakTests.js"
nft_test="nftMarketplace/nftMarketplaceTests.js"

# Build contracts repo first since we rely on that for lib.js
cd contracts
npm ci

cd ../integration_test/dapp_tests
npm ci

npx hardhat compile

# Set the CONFIG environment variable
export DAPP_TEST_ENV=$1

# Determine which tests to run
if [ -z "$2" ]; then
  tests=("$uniswap_test" "$steak_test" "$nft_test")
else
  case $2 in
    uniswap)
      tests=("$uniswap_test")
      ;;
    steak)
      tests=("$steak_test")
      ;;
    nft)
      tests=("$nft_test")
      ;;
    *)
      echo "Invalid test specified. Please choose either 'uniswap', 'steak', or 'nft'."
      exit 1
      ;;
  esac
fi

# Run the selected tests
for test in "${tests[@]}"; do
  npx hardhat test --network $1 $test
done

