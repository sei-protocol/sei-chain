#!/usr/bin/env sh

NODE_ID=${ID:-0}
# Build on node 0
if [ $NODE_ID = 0 ] && [ -z $SKIP_BUILD ]
then
  /usr/bin/build.sh
fi

if ! [ $SKIP_BUILD ]
then
  until [ -f build/generated/build.complete ]
  do
       sleep 5
  done
fi

# Run init on all nodes
/usr/bin/configure_init.sh

# Genesis on node 0
if [ $NODE_ID = 0 ]
then
  echo "Running genesis on node 0"
  /usr/bin/genesis.sh
fi

until [ -f build/generated/genesis-sei.json ]
do
     sleep 5
done

# Configure persistent peers
/usr/bin/persistent_peers.sh

# Start the chain
/usr/bin/start_sei.sh