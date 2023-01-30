#!/usr/bin/env sh

NODE_ID=${ID:-0}
# Set up env
export GOPATH=$HOME/go
export GOBIN=$GOPATH/bin
export BUILD_PATH=/sei-protocol/sei-chain/build
export PATH=$GOBIN:$PATH:/usr/local/go/bin:$BUILD_PATH
echo "export GOPATH=$HOME/go" >> /root/.bashrc
echo "GOBIN=$GOPATH/bin" >> /root/.bashrc
echo "export PATH=$GOBIN:$PATH:/usr/local/go/bin:$BUILD_PATH" >> /root/.bashrc
/bin/bash -c "source /root/.bashrc"
mkdir -p $GOBIN
# Step 1 build seid
/usr/bin/build.sh

# Run init to set up state sync configurations
/usr/bin/configure_init.sh

# Start the chain
/usr/bin/start_sei.sh
