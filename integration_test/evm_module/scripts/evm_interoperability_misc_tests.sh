#!/bin/bash

set -e

cd contracts
npm ci
npx hardhat test --network seilocal test/SeiSoloTest.js
npx hardhat test --network seilocal test/SetCodeTxTest.js
npx hardhat test --network seilocal test/TransientStorageTest.js
