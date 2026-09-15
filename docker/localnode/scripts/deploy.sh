#!/usr/bin/env sh
set -e

NODE_ID=${ID:-0}
CLUSTER_SIZE=${CLUSTER_SIZE:-1}
PHASE=${AUTOBAHN_E2E_PHASE:-all}

# Clean up and env set up
export GOPATH=$HOME/go
export GOBIN=$GOPATH/bin
export BUILD_PATH=/sei-protocol/sei-chain/build
export PATH=$GOBIN:$PATH:/usr/local/go/bin:$BUILD_PATH
echo "export GOPATH=$HOME/go" >> "$HOME/.bashrc"
echo "GOBIN=$GOPATH/bin" >> "$HOME/.bashrc"
echo "export PATH=$GOBIN:$PATH:/usr/local/go/bin:$BUILD_PATH:$HOME/.foundry/bin" >> "$HOME/.bashrc"
/bin/bash -c "source $HOME/.bashrc"
mkdir -p $GOBIN

# build/generated is a shared bind mount across all localnode containers. The
# Makefile cluster targets clean it before docker compose starts; deleting it
# here races with other nodes writing init/genesis/launch coordination files.
mkdir -p build/generated

ensure_seid() {
  if [ -f build/seid ]; then
    cp build/seid "$GOBIN"/
  fi
}

run_build() {
  if [ -n "$SKIP_BUILD" ]
  then
    return
  fi
  # Local 4-in-1 compose shares build/; only node 0 compiles. AWS init
  # runs one node per host, so every PHASE=init container compiles itself.
  if [ "$NODE_ID" = 0 ] || [ "$PHASE" = "init" ]
  then
    /usr/bin/build.sh $MOCK_BALANCES
  fi
  if [ "$PHASE" = "all" ]
  then
    until [ -f build/generated/build.complete ]
    do
         sleep 1
    done
  fi
}

run_init() {
  /usr/bin/configure_init.sh
}

run_genesis() {
  if [ "$NODE_ID" != 0 ]
  then
    return
  fi
  if [ "$PHASE" = "all" ]
  then
    until [ -f build/generated/init.complete ]
    do
         sleep 1
    done
    while [ $(cat build/generated/init.complete |wc -l) -lt "$CLUSTER_SIZE" ]
    do
         sleep 1
    done
  fi
  echo "Running genesis on node 0"
  /usr/bin/genesis.sh
}

run_start() {
  until [ -f build/generated/genesis.json ]
  do
       sleep 1
  done

  /usr/bin/config_override.sh
  /usr/bin/start_sei.sh

  if [ "$PHASE" = "all" ]
  then
    while [ $(cat build/generated/launch.complete |wc -l) -lt "$CLUSTER_SIZE" ]
    do
      sleep 1
    done
    sleep 5
    echo "All $CLUSTER_SIZE Nodes started successfully."
  fi
  tail -f /dev/null
}

case "$PHASE" in
  init)
    run_build
    run_init
    echo "init phase complete for node $NODE_ID"
    ;;
  genesis)
    ensure_seid
    run_genesis
    echo "genesis phase complete"
    ;;
  start)
    ensure_seid
    run_start
    ;;
  all)
    run_build
    run_init
    run_genesis
    run_start
    ;;
  *)
    echo "unknown AUTOBAHN_E2E_PHASE=$PHASE" >&2
    exit 1
    ;;
esac
