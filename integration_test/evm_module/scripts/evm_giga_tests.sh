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

# Ensure Node.js >= 18 (required by hardhat and esbuild)
if [ -s "$HOME/.nvm/nvm.sh" ]; then
  source "$HOME/.nvm/nvm.sh"
  nvm use 22 2>/dev/null || nvm use 20 2>/dev/null || nvm use 18 2>/dev/null || true
fi
echo "Using Node.js $(node --version)"

echo "=== Running GIGA EVM Tests ==="

cd contracts

# Clean install - remove cached modules to avoid version conflicts
rm -rf node_modules/.cache
npm ci --prefer-offline || npm install

# Run the GIGA-specific tests
npx hardhat test --network seilocal test/EVMGigaTest.js

echo "=== GIGA EVM Tests Complete ==="
