#!/bin/bash

make clean

# Check if $1 is set and use its value for UPGRADE_VERSION_LIST
if [ -n "$1" ]; then
    INVARIANT_CHECK_INTERVAL=10 UPGRADE_VERSION_LIST=$1 make docker-cluster-start &
else
    INVARIANT_CHECK_INTERVAL=10 make docker-cluster-start &
fi

# wait for launch.complete
until [ $(cat build/generated/launch.complete | wc -l) = 4 ]
do
  sleep 10
done
sleep 10

# launch RPC node
make run-rpc-node-skipbuild &

sleep 5

python3 integration_test/scripts/runner.py integration_test/startup/startup_test.yaml
