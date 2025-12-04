#!/bin/bash

set -e

cd contracts
npm ci
npx hardhat test --network seilocal test/EVMCompatabilityTest.js
npx hardhat test --network seilocal test/EVMPrecompileTest.js
npx hardhat test --network seilocal test/SeiEndpointsTest.js
npx hardhat test --network seilocal test/AssociateTest.js