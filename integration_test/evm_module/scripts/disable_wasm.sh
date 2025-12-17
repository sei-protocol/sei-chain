#!/bin/bash

set -e

cd contracts
npm ci
npx hardhat test --network seilocal test/DisableWasmTest.js