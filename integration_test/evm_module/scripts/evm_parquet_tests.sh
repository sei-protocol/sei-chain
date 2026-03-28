#!/bin/bash
#
# Parquet Receipt Store Integration Tests
#
# This script runs EVM receipt/log tests against a cluster using the parquet
# receipt store backend. The cluster must be started with RECEIPT_BACKEND=parquet
# either via the docker-compose.parquet.yml overlay or by setting the env var.
#
# For local testing, start the cluster manually with:
#   make parquet-integration-test
#

set -e

echo "=== Running Parquet Receipt Store EVM Tests ==="

cd contracts

# Clean install
rm -rf node_modules/.cache
npm ci --prefer-offline || npm install

# Run the parquet-specific receipt/log tests
npx hardhat test --network seilocal test/ParquetReceiptTest.js

echo "=== Parquet Receipt Store EVM Tests Complete ==="
