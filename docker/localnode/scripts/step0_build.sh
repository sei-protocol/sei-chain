#!/usr/bin/env sh

# Input parameters
NODE_ID=${ID:-0}

echo $"Build seid from local branch"
# Build seid
LEDGER_ENABLED=false
make clean
make build-linux
mkdir -p build/generated
echo "DONE" > build/generated/build.complete