#!/bin/bash

set -e

cd contracts

for attempt in 1 2 3; do
  if npm ci; then
    break
  fi

  if [ "$attempt" -eq 3 ]; then
    exit 1
  fi

  echo "npm ci failed; retrying in $((attempt * 5)) seconds..."
  rm -rf node_modules
  sleep $((attempt * 5))
done

npx hardhat test --network seilocal test/DisableWasmTest.js
