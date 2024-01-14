#!/usr/bin/env sh

# Input parameters
NODE_ID=${ID:-0}
ARCH=$(uname -m)

# Build seid
echo "Building seid from local branch"
git config --global --add safe.directory /sei-protocol/sei-chain
LEDGER_ENABLED=false
make clean
make build-linux
make build-price-feeder-linux
mkdir -p build/generated
# echo "install foundry" > build/generated/build.complete
echo "installing foundry"
curl -L https://foundry.paradigm.xyz | bash
echo "sourcing bashrc"
. /root/.bashrc
echo "running foundryup"
foundryup
# RUN curl -L https://foundry.paradigm.xyz | bash > build/generated/foundry.log 2>&1
# echo "sourcing bashrc" >> build/generated/foundry.log
# RUN source /root/.bashrc >> build/generated/foundry.log 2>&1
# echo "running foundryup" >> build/generated/foundry.log
# RUN foundryup >> build/generated/foundry.log 2>&1
echo "DONE" > build/generated/build.complete

# install foundry
# TODO: can move to own script
# RUN curl -L https://foundry.paradigm.xyz | bash && source /root/.bashrc && foundryup
