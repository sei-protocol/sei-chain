#!/usr/bin/env sh

# Input parameters
NODE_ID=${ID:-0}
ARCH=$(uname -m)

# Build seid
echo "Building seid from local branch"
git config --global --add safe.directory /sei-protocol/sei-chain
export LEDGER_ENABLED=false
make clean
# build seid with the mock balance function enabled
make build-linux LDFLAGS="-X github.com/sei-protocol/sei-chain/x/evm/state.mockBalanceTesting=enabled"
make build-price-feeder-linux
mkdir -p build/generated
echo "DONE" > build/generated/build.complete
