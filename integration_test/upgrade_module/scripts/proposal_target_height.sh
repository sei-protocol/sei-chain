#!/bin/bash

# Number of seconds passed as an argument
NUMBER_OF_SECONDS=$1

# Calculate the number of blocks based on 300ms block time
NUMBER_OF_BLOCKS=$((NUMBER_OF_SECONDS * 1000 / 300))

# Get the current height and add the calculated number of blocks
HEIGHT=$(seid status | jq -r '.SyncInfo.latest_block_height' | awk -v blocks="$NUMBER_OF_BLOCKS" '{print $1 + blocks}')

echo $HEIGHT
