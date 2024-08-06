#!/bin/bash

set -e

# Build contacts repo first since we rely on that for lib.js
cd contracts
npm ci

cd ../integration_test/dapp_tests
npm ci

npx hardhat compile

npx hardhat test --network seilocal uniswap/uniswapTest.js
