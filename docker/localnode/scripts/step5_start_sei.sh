#!/usr/bin/env sh

NODE_ID=${ID:-0}

LOG_DIR="build/generated/logs"
mkdir -p $LOG_DIR

# Starting sei chain
echo "Starting the sei chain daemon"
cp build/generated/genesis-sei.json ~/.sei/config/genesis.json
./build/seid start --chain-id sei > "$LOG_DIR"/seid-$NODE_ID.log 2>&1 &
echo "Done" >> build/generated/launch.complete

echo "SeiNode $NODE_ID started successfully! Check your logs under $LOG_DIR/"

until [ $(cat build/generated/launch.complete |wc -l) = 4 ]
do
  sleep 5
done

echo "All 4 Sei Nodes started successfully"


tail -f /dev/null