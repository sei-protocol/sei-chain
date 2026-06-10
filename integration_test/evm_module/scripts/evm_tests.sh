#!/bin/bash

set -e

cd contracts
npm ci

run_test() {
  local file=$1
  local start=$(date +%s)
  npx hardhat test --network seilocal "$file"
  echo "[timing] $file: $(($(date +%s) - start))s"
}

run_test test/EVMCompatabilityTest.js
run_test test/EVMPrecompileTest.js
run_test test/SeiEndpointsTest.js
run_test test/AssociateTest.js
