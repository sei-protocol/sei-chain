#!/bin/bash
# Usage: ./statesynctrustha.sh <RPC URL>
# Example: ./statesynctrustha.sh https://rpc.sei-apis.com

RPCADDR=$1
DAEMON="sei"

# Get the latest block height
LATEST_BLOCK_HEIGHT=$(curl -s "$RPCADDR/status" | jq -r .sync_info.latest_block_height)

# Calculate the block height for the trust hash query (latest - 5000)
TRUST_HEIGHT=$((LATEST_BLOCK_HEIGHT - 5000))

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
