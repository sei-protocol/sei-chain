#!/bin/bash
# Usage: ./statesynctrustha.sh <RPC URL>
# Example: ./statesynctrustha.sh https://rpc.sei-apis.com

RPCADDR=$1
DAEMON="sei"

# Set /tmp as a 12G RAM disk to allow for more than 400 state sync chunks
sudo umount -l /tmp && sudo mount -t tmpfs -o size=12G,mode=1777 overflow /tmp

# Get the latest block height
LATEST_BLOCK_HEIGHT=$(curl -s "$RPCADDR/status" | jq -r .sync_info.latest_block_height)

# Fetch the latest block height from the State Sync RPC endpoint
LATEST_HEIGHT=$(curl -s $STATE_SYNC_RPC/block | jq -r .result.block.header.height)

# Calculate the trust height (rounded down to the nearest 100,000)
BLOCK_HEIGHT=$(( (LATEST_HEIGHT / 100000) * 100000 ))

# Fetch the block hash at the trust height
TRUST_HASH=$(curl -s "$STATE_SYNC_RPC/block?height=$BLOCK_HEIGHT" | jq -r .result.block_id.hash)

# Get the trust hash at the calculated block height
TRUST_HASH=$(curl -s "$RPCADDR/block?height=$TRUST_HEIGHT" | jq -r .block_id.hash)

# Output the TRUST_HEIGHT and TRUST_HASH
echo "TRUST_HEIGHT: $TRUST_HEIGHT"
echo "TRUST_HASH: $TRUST_HASH"

# Path to the config.toml file
CONFIG_FILE="$HOME/.sei/config/config.toml"

# Update config.toml with TRUST_HEIGHT, TRUST_HASH, RPC servers, and chunk fetchers
sed -i "s|^enable = .*|enable = true|" "$CONFIG_FILE"
sed -i "s|^use-p2p = .*|use-p2p = false|" "$CONFIG_FILE"
sed -i "s|^rpc-servers = .*|rpc-servers = \"$RPCADDR,$RPCADDR\"|" "$CONFIG_FILE"
sed -i "s|^trust_height = .*|trust_height = $TRUST_HEIGHT|" "$CONFIG_FILE"
sed -i "s|^trust_hash = .*|trust_hash = \"$TRUST_HASH\"|" "$CONFIG_FILE"
sed -i "s|^fetchers = .*|fetchers = \"4\"|" "$CONFIG_FILE"

echo "Updated $CONFIG_FILE with TRUST_HEIGHT, TRUST_HASH, RPC server settings, and chunk fetchers set to 4."
