#!/bin/bash

set -e

cd integration_test/dapp_tests/uniswap
npm ci

npx hardhat compile

npx hardhat test --network seilocal uniswapTest.js
