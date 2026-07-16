#!/bin/bash

set -e

cd contracts
npm ci

cd ../integration_test/dapp_tests
npm ci
npx hardhat compile

export DAPP_TEST_ENV=seilocal
npx hardhat test --network seilocal nftMarketplace/nftMarketplaceTests.js
npx hardhat test --network seilocal steak/SteakTests.js
