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

# Apply Giga Storage overrides so the RPC node's app hash matches the validators.
GIGA_STORAGE=${GIGA_STORAGE:-false}
if [ "$GIGA_STORAGE" = "true" ]; then
  echo "Enabling Giga Storage for RPC node..."

  # SC layer: must match validators (dual_write + split_read + lattice hash)
  if grep -q "sc-write-mode" ~/.sei/config/app.toml; then
    sed -i 's/sc-write-mode = .*/sc-write-mode = "dual_write"/' ~/.sei/config/app.toml
  else
    sed -i '/^\[state-store\]/i sc-write-mode = "dual_write"' ~/.sei/config/app.toml
  fi
  if grep -q "sc-read-mode" ~/.sei/config/app.toml; then
    sed -i 's/sc-read-mode = .*/sc-read-mode = "split_read"/' ~/.sei/config/app.toml
  else
    sed -i '/^\[state-store\]/i sc-read-mode = "split_read"' ~/.sei/config/app.toml
  fi
  if grep -q "sc-enable-lattice-hash" ~/.sei/config/app.toml; then
    sed -i 's/sc-enable-lattice-hash = .*/sc-enable-lattice-hash = true/' ~/.sei/config/app.toml
  else
    sed -i '/^\[state-store\]/i sc-enable-lattice-hash = true' ~/.sei/config/app.toml
  fi

  # SS layer: EVM split_write + split_read
  sed -i 's/evm-ss-write-mode = .*/evm-ss-write-mode = "split_write"/' ~/.sei/config/app.toml
  sed -i 's/evm-ss-read-mode = .*/evm-ss-read-mode = "split_read"/' ~/.sei/config/app.toml
fi

# Apply receipt backend override if requested
RECEIPT_BACKEND=${RECEIPT_BACKEND:-}
if [ -n "$RECEIPT_BACKEND" ]; then
  echo "Setting receipt store backend to '$RECEIPT_BACKEND' for RPC node..."
  if grep -q "\[receipt-store\]" ~/.sei/config/app.toml; then
    sed -i "s/rs-backend = .*/rs-backend = \"$RECEIPT_BACKEND\"/" ~/.sei/config/app.toml
  else
    echo "" >> ~/.sei/config/app.toml
    echo "[receipt-store]" >> ~/.sei/config/app.toml
    echo "rs-backend = \"$RECEIPT_BACKEND\"" >> ~/.sei/config/app.toml
  fi
fi

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
