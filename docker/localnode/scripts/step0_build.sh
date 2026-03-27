#!/usr/bin/env sh

# Input parameters
NODE_ID=${ID:-0}
ARCH=$(uname -m)
MOCK_BALANCES=${MOCK_BALANCES:-true}

# Build seid
echo "Building seid from local branch"
git config --global --add safe.directory /sei-protocol/sei-chain
export LEDGER_ENABLED=false
make clean
# build seid with the mock balance function enabled
if [ "$MOCK_BALANCES" = true ]; then
    echo "Building with mock balances enabled..."
    make build-linux BUILD_TAGS="mock_balances"
else
    echo "Building with standard configuration..."
    make build-linux
fi
mkdir -p build/generated
echo "DONE" > build/generated/build.complete
