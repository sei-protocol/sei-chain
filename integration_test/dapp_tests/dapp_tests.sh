#!/bin/bash

set -e

cd integration_test/dapp_tests
npm ci

npx hardhat compile

npx hardhat test --network seilocal uniswap/uniswapTest.js
