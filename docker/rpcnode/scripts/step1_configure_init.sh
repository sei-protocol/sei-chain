#!/usr/bin/env sh

# Set up GO PATH
echo "Configure and initialize environment"

# Testing whether seid works or not
seid version # Uncomment the below line if there are any dependency issues
# ldd build/seid

# Initialize validator node
MONIKER="sei-rpc-node"
seid init --chain-id sei "$MONIKER"

# Reuse the pre-generated rpc-only peer node key, if present. Each validator's
# autobahn-rpc-only-peers config lists this key's public half (see localnode
# step1 + step4); the rpc node must boot with the matching private key or
# inbound block-sync connections will be rejected as "peer not whitelisted".
RPC_PEER_KEY_SRC="build/generated/rpc_node/config/node_key.json"
if [ -f "$RPC_PEER_KEY_SRC" ]; then
  cp "$RPC_PEER_KEY_SRC" ~/.sei/config/node_key.json
  echo "Reused pre-generated rpc-only peer node key from $RPC_PEER_KEY_SRC"
fi

# Copy configs
cp docker/rpcnode/config/app.toml ~/.sei/config/app.toml
cp docker/rpcnode/config/config.toml ~/.sei/config/config.toml
cp build/generated/genesis.json ~/.sei/config/genesis.json

# Apply Giga Storage overrides so the RPC node's app hash matches the validators.
GIGA_STORAGE=${GIGA_STORAGE:-false}
if [ "$GIGA_STORAGE" = "true" ]; then
  # Default receipt backend to parquet when giga storage is on; callers may
  # still override via an explicit RECEIPT_BACKEND env var.
  RECEIPT_BACKEND=${RECEIPT_BACKEND:-parquet}
  echo "Enabling Giga Storage for RPC node..."

  # SC layer: must match validators (test_only_dual_write)
  if grep -q "sc-write-mode" ~/.sei/config/app.toml; then
    sed -i 's/sc-write-mode = .*/sc-write-mode = "test_only_dual_write"/' ~/.sei/config/app.toml
  else
    sed -i '/^\[state-store\]/i sc-write-mode = "test_only_dual_write"' ~/.sei/config/app.toml
  fi

  # SS layer: enable EVM split
  sed -i 's/evm-ss-split = .*/evm-ss-split = true/' ~/.sei/config/app.toml
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

# Generate Autobahn (GigaRouter) config when the validators are running
# Autobahn consensus. The RPC node uses mode = "full" (see config.toml),
# which makes it an rpc-only autobahn participant — loads the committee
# for routing only and forwards eth_sendRawTransaction to the shard owner.
# Reuse the validator node directories under build/generated/ (mounted
# into the container) so the committee description matches the cluster.
AUTOBAHN=${AUTOBAHN:-false}
if [ "$AUTOBAHN" = "true" ]; then
  echo "Generating Autobahn config for RPC node (rpc-only via mode=full)..."
  AUTOBAHN_CONFIG="$HOME/.sei/config/autobahn.json"

  # Default to 4 (the docker-compose cluster size) when CLUSTER_SIZE is unset.
  CLUSTER_SIZE=${CLUSTER_SIZE:-4}
  NODE_DIRS=""
  i=0
  while [ "$i" -lt "$CLUSTER_SIZE" ]; do
    NODE_DIRS="$NODE_DIRS build/generated/node_${i}"
    i=$((i + 1))
  done

  seid tendermint gen-autobahn-config $NODE_DIRS --output "$AUTOBAHN_CONFIG"

  # Inject autobahn-config-file as a top-level key in config.toml. It must
  # precede any [section] header so the TOML parser sees it at root scope.
  if grep -q "autobahn-config-file" ~/.sei/config/config.toml; then
    sed -i 's|autobahn-config-file = .*|autobahn-config-file = "'"$AUTOBAHN_CONFIG"'"|' ~/.sei/config/config.toml
  else
    sed -i '1s|^|autobahn-config-file = "'"$AUTOBAHN_CONFIG"'"\n|' ~/.sei/config/config.toml
  fi
  echo "Autobahn config written to $AUTOBAHN_CONFIG (rpc-only via mode=full)"
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
