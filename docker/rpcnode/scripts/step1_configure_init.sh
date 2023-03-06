#!/usr/bin/env sh

# Set up GO PATH
echo "Configure and initialize environment"

# Testing whether seid works or not
seid version # Uncomment the below line if there are any dependency issues
# ldd build/seid

# Initialize validator node
MONIKER="sei-rpc-node"
seid init --chain-id sei "$MONIKER"

# Copy configs
cp docker/rpcnode/config/app.toml ~/.sei/config/app.toml
cp docker/rpcnode/config/config.toml ~/.sei/config/config.toml
cp build/generated/genesis.json ~/.sei/config/genesis.json

# Override state sync configs
STATE_SYNC_RPC="192.168.10.10:26657"
STATE_SYNC_PEER="2f9846450b7a3dcf4af1ac0082e3279c16744df8@172.31.9.18:26656,ec98c4a28a2023f4f976828c8a8e7127bfef4e1b@172.31.4.96:26656,b03014d67384fb0ef6ad992c77cefe4f9d2c1640@172.31.4.219:26656"
curl "$STATE_SYNC_RPC"/net_info |jq -r '.peers[] | .url' |sed -e 's#mconn://##' >> build/generated/PEERS
STATE_SYNC_PEER=$(paste -s -d ',' build/generated/PEERS)
LATEST_HEIGHT=$(curl -s $STATE_SYNC_RPC/block | jq -r .block.header.height)
SYNC_BLOCK_HEIGHT=$LATEST_HEIGHT
SYNC_BLOCK_HASH=$(curl -s "$STATE_SYNC_RPC/block?height=$SYNC_BLOCK_HEIGHT" | jq -r .block_id.hash)
sed -i.bak -e "s|^enable *=.*|enable = true|" ~/.sei/config/config.toml
sed -i.bak -e "s|^rpc-servers *=.*|rpc-servers = \"$STATE_SYNC_RPC,$STATE_SYNC_RPC\"|" ~/.sei/config/config.toml
sed -i.bak -e "s|^trust-height *=.*|trust-height = $SYNC_BLOCK_HEIGHT|" ~/.sei/config/config.toml
sed -i.bak -e "s|^trust-hash *=.*|trust-hash = \"$SYNC_BLOCK_HASH\"|" ~/.sei/config/config.toml
sed -i.bak -e "s|^persistent-peers *=.*|persistent-peers = \"$STATE_SYNC_PEER\"|" ~/.sei/config/config.toml
