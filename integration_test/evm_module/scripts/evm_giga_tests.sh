#!/bin/bash
#
# GIGA EVM Integration Tests
#
# This script runs EVM tests against a GIGA-enabled cluster.
# The cluster is started by the GitHub workflow with GIGA_EXECUTOR=true GIGA_OCC=true
# passed via the env field in the workflow matrix.
#
# For local testing, start the cluster manually with:
#   GIGA_EXECUTOR=true GIGA_OCC=true make docker-cluster-start
#

set -e

echo "=== Running GIGA EVM Tests ==="

cd contracts

# Clean install - remove cached modules to avoid version conflicts
rm -rf node_modules/.cache
npm ci --prefer-offline || npm install

# Run the GIGA-specific tests
npx hardhat test --network seilocal test/EVMGigaTest.js

echo "=== GIGA EVM Tests Complete ==="
