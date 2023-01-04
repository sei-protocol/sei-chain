#!/bin/bash

# Verify there are 4 validators
NUM_VALIDATORS=$(docker exec -i sei-node0 build/seid q tendermint-validator-set  |grep address |wc -l)
if [[ "$NUM_VALIDATORS" -eq 4 ]];
then
  echo "Number validators is correct: $NUM_VALIDATORS"
else
  exit 1
fi

# Verify block height is keep increasing
HEIGHT_1=$(docker exec -i sei-node0 build/seid status |jq -r .SyncInfo.latest_block_height)
echo $HEIGHT_1
sleep 15
HEIGHT_2=$(docker exec -i sei-node0 build/seid status |jq -r .SyncInfo.latest_block_height)
echo $HEIGHT_2
if [ "$HEIGHT_1" -ge "$HEIGHT_2" ];
then
  exit 1
fi