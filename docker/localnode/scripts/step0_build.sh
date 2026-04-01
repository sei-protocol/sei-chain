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
# ENABLE_BENCHMARK=true adds the benchmark build tag (in-process load + mempool pump).
# Otherwise use mock_balances like the default localnet image.
ENABLE_BENCHMARK=${ENABLE_BENCHMARK:-false}
if [ "$ENABLE_BENCHMARK" = true ]; then
    echo "Building with benchmark + mock_balances enabled..."
    make build-linux BUILD_TAGS="mock_balances benchmark"
elif [ "$MOCK_BALANCES" = true ]; then
    echo "Building with mock balances enabled..."
    make build-linux BUILD_TAGS="mock_balances"
else
    echo "Building with standard configuration..."
    make build-linux
fi
mkdir -p build/generated
echo "DONE" > build/generated/build.complete
