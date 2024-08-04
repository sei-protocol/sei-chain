#!/bin/bash

set -e

cd contracts
npm ci

cd ..

cd integration_test/dapp_tests/uniswap
npm ci

npx hardhat compile

npx hardhat test --network seilocal uniswapTest.js
