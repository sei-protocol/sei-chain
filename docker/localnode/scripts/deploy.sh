#!/usr/bin/env sh

NODE_ID=${ID:-0}
CLUSTER_SIZE=${CLUSTER_SIZE:-1}

# Clean up and env set up
export GOPATH=$HOME/go
export GOBIN=$GOPATH/bin
export BUILD_PATH=/sei-protocol/sei-chain/build
export PATH=$GOBIN:$PATH:/usr/local/go/bin:$BUILD_PATH
# So prebuilt seid (built on runner) finds all libwasmvm*.so in mounted repo at runtime
WASMVM_LIBS="/sei-protocol/sei-chain/sei-wasmvm/internal/api:/sei-protocol/sei-chain/sei-wasmd/x/wasm/artifacts/v152/api:/sei-protocol/sei-chain/sei-wasmd/x/wasm/artifacts/v155/api"
export LD_LIBRARY_PATH="${WASMVM_LIBS}${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
echo "export GOPATH=$HOME/go" >> "$HOME/.bashrc"
echo "GOBIN=$GOPATH/bin" >> "$HOME/.bashrc"
echo "export PATH=$GOBIN:$PATH:/usr/local/go/bin:$BUILD_PATH:$HOME/.foundry/bin" >> "$HOME/.bashrc"
echo "export LD_LIBRARY_PATH=\"${WASMVM_LIBS}:\$LD_LIBRARY_PATH\"" >> "$HOME/.bashrc"
rm -rf build/generated
/bin/bash -c "source $HOME/.bashrc"
mkdir -p $GOBIN

# Use prebuilt binaries from the image when present (CI: binary baked in; no in-container build)
if [ -f /prebuilt/seid ]; then
  mkdir -p build/generated
  cp -f /prebuilt/seid build/seid
  cp -f /prebuilt/price-feeder build/price-feeder
  echo "DONE" > build/generated/build.complete
  export SKIP_BUILD=1
fi

# Step 0: Build on node 0 (skipped when SKIP_BUILD is set from prebuilt above)
if [ "$NODE_ID" = 0 ] && [ -z "$SKIP_BUILD" ]
then
  /usr/bin/build.sh $MOCK_BALANCES
fi

if ! [ "$SKIP_BUILD" ]
then
  until [ -f build/generated/build.complete ]
  do
       sleep 1
  done
fi

# Step 1: Run init on all nodes
/usr/bin/configure_init.sh

# Step 2&3: Genesis on node 0
if [ "$NODE_ID" = 0 ]
then
  # wait for other nodes init complete
  until [ -f build/generated/init.complete ]
  do
       sleep 1
  done
  while [ $(cat build/generated/init.complete |wc -l) -lt "$CLUSTER_SIZE" ]
  do
       sleep 1
  done
  echo "Running genesis on node 0"
  /usr/bin/genesis.sh
fi

until [ -f build/generated/genesis.json ]
do
     sleep 1
done

# Step 4: Config overrides
/usr/bin/config_override.sh

# Step 5: Start the chain
/usr/bin/start_sei.sh

# Wait until the chain started
while [ $(cat build/generated/launch.complete |wc -l) -lt "$CLUSTER_SIZE" ]
do
  sleep 1
done
sleep 5
echo "All $CLUSTER_SIZE Nodes started successfully, starting oracle price feeder..."

# Step 6: Start oracle price feeder
if ! /usr/bin/start_price_feeder.sh; then
  echo "Failed to start oracle price feeder on node $NODE_ID" >&2
  exit 1
fi
echo "Oracle price feeder is started"

tail -f /dev/null
