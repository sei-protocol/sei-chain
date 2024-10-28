#!/usr/bin/env sh

NODE_ID=${ID:-0}
CLUSTER_SIZE=${CLUSTER_SIZE:-1}

# Clean up and env set up
export GOPATH=$HOME/go
export GOBIN=$GOPATH/bin
export BUILD_PATH=/sei-protocol/sei-chain/build
export PATH=$GOBIN:$PATH:/usr/local/go/bin:$BUILD_PATH
echo "export GOPATH=$HOME/go" >> "$HOME/.bashrc"
echo "GOBIN=$GOPATH/bin" >> "$HOME/.bashrc"
echo "export PATH=$GOBIN:$PATH:/usr/local/go/bin:$BUILD_PATH:$HOME/.foundry/bin" >> "$HOME/.bashrc"
rm -rf build/generated
/bin/bash -c "source $HOME/.bashrc"
mkdir -p $GOBIN
# Step 0: Build on node 0
if [ "$NODE_ID" = 0 ] && [ -z "$SKIP_BUILD" ]
then
  /usr/bin/build.sh
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
/usr/bin/start_price_feeder.sh
echo "Oracle price feeder is started"

tail -f /dev/null
