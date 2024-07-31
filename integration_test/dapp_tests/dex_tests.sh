#!/bin/bash

set -e

cd integration_test
npm ci
npx hardhat run --network seilocal dapp_tests/scripts/deployUniswapV3.js