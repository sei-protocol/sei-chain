#!/usr/bin/env sh

# Input parameters
NODE_ID=${ID:-0}
ARCH=$(uname -m)
MOCK_BALANCES=${MOCK_BALANCES:-false}

# Build seid
echo "Building seid from local branch"
git config --global --add safe.directory /sei-protocol/sei-chain
export LEDGER_ENABLED=false
make clean
# build seid with the mock balance function enabled
if [ "$MOCK_BALANCES" = true ]; then
    echo "Building with mock balances enabled..."
    make build-linux LDFLAGS="-X github.com/sei-protocol/sei-chain/x/evm/state.mockBalanceTesting=enabled"
else
    echo "Building with standard configuration..."
    make build-linux
fi
make build-price-feeder-linux
mkdir -p build/generated
echo "DONE" > build/generated/build.complete
