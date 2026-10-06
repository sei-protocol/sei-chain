#!/bin/bash

set -e

cd contracts
npm ci
# Skipped internally on the EVM-only Autobahn chain (reported as pending): Sei
# Solo claims move Sei account and CosmWasm balances, which that chain lacks.
npx hardhat test --network seilocal test/SeiSoloTest.js
npx hardhat test --network seilocal test/SetCodeTxTest.js
npx hardhat test --network seilocal test/TransientStorageTest.js
