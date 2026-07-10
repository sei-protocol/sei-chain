#!/usr/bin/env sh
#
# rpcnode init script for the integration-test docker setup. NOT a
# production deploy script. Best-effort by design — curl probes against
# the validator RPC are allowed to fail (the script proceeds with
# whatever values came back, the container then converges as the cluster
# stabilises). The few cases where we DO want fail-fast — genesis file
# missing after the bounded wait, autobahn validator dirs missing — are
# guarded by explicit `if [ ! -f ]; exit 1` checks below. Don't add
# `set -e`: it makes those transient curl probes fatal and breaks
# init when the cluster is still warming up.

# Set up GO PATH
echo "Configure and initialize environment"

# Testing whether seid works or not
seid version # Uncomment the below line if there are any dependency issues
# ldd build/seid

# Initialize validator node. --overwrite so this is safe to re-run inside
# a recycled container; the script writes new configs over whatever was
# already on the previous run.
MONIKER="sei-rpc-node"
seid init --overwrite --chain-id sei "$MONIKER"

# Wait for the chain genesis.json (validator step3 writes it). The test
# setup may spawn the rpc node in parallel with the cluster, so this can
# still be missing here — poll up to 5 minutes.
GENESIS_SRC="build/generated/genesis.json"
i=0
while [ ! -f "$GENESIS_SRC" ] && [ "$i" -lt 300 ]; do
  sleep 1
  i=$((i + 1))
done
if [ ! -f "$GENESIS_SRC" ]; then
  echo "ERROR: $GENESIS_SRC missing after 5 minutes; aborting." >&2
  exit 1
fi

# Copy configs
cp docker/rpcnode/config/app.toml ~/.sei/config/app.toml
cp docker/rpcnode/config/config.toml ~/.sei/config/config.toml
cp "$GENESIS_SRC" ~/.sei/config/genesis.json

# pin_sc_write_mode MODE: set sc-write-mode = "MODE" and pin
# sc-write-mode-enable-auto = false so the explicit mode is actually honored.
# sc-write-mode-enable-auto defaults to true, which forces the node into auto
# and ignores any explicit sc-write-mode; the RPC node must match the
# validators' pinned backend exactly, so it must opt out of auto.
pin_sc_write_mode() {
  mode="$1"
  cfg="$HOME/.sei/config/app.toml"
  if grep -q '^sc-write-mode[[:space:]]*=' "$cfg"; then
    sed -i "s/^sc-write-mode[[:space:]]*=.*/sc-write-mode = \"$mode\"/" "$cfg"
  else
    sed -i "/^\[state-store\]/i sc-write-mode = \"$mode\"" "$cfg"
  fi
  if grep -q '^sc-write-mode-enable-auto[[:space:]]*=' "$cfg"; then
    sed -i "s/^sc-write-mode-enable-auto[[:space:]]*=.*/sc-write-mode-enable-auto = false/" "$cfg"
  elif grep -q '^sc-write-mode[[:space:]]*=' "$cfg"; then
    sed -i "/^sc-write-mode[[:space:]]*=/a sc-write-mode-enable-auto = false" "$cfg"
  else
    sed -i "/^\[state-store\]/i sc-write-mode-enable-auto = false" "$cfg"
  fi
}

# Apply Giga Storage overrides so the RPC node's app hash matches the validators.
GIGA_STORAGE=${GIGA_STORAGE:-false}
GIGA_FLATKV_ONLY=${GIGA_FLATKV_ONLY:-false}
if [ "$GIGA_STORAGE" = "true" ] && [ "$GIGA_FLATKV_ONLY" != "true" ]; then
  # Default receipt backend to pebble when giga storage is on; callers may
  # still override via an explicit RECEIPT_BACKEND env var.
  RECEIPT_BACKEND=${RECEIPT_BACKEND:-pebble}
  echo "Enabling Giga Storage for RPC node..."

  # SC layer: must match validators (test_only_dual_write)
  pin_sc_write_mode "test_only_dual_write"

  # SS layer: enable EVM split
  sed -i 's/^evm-ss-split[[:space:]]*=.*/evm-ss-split = true/' ~/.sei/config/app.toml
fi

if [ "$GIGA_FLATKV_ONLY" = "true" ]; then
  echo "Booting RPC node in flatkv_only mode..."
  pin_sc_write_mode "flatkv_only"
  sed -i 's/^evm-ss-split[[:space:]]*=.*/evm-ss-split = false/' ~/.sei/config/app.toml
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
# which makes it an fullnode autobahn participant — loads the committee
# for routing only and forwards eth_sendRawTransaction to the shard owner.
# Reuse the validator node directories under build/generated/ (mounted
# into the container) so the committee description matches the cluster.
AUTOBAHN=${AUTOBAHN:-false}
if [ "$AUTOBAHN" = "true" ]; then
  echo "Generating Autobahn config for RPC node (fullnode via mode=full)..."
  AUTOBAHN_CONFIG="$HOME/.sei/config/autobahn.json"

  # Default to 4 (the docker-compose cluster size) when CLUSTER_SIZE is unset.
  CLUSTER_SIZE=${CLUSTER_SIZE:-4}
  NODE_DIRS=""
  i=0
  while [ "$i" -lt "$CLUSTER_SIZE" ]; do
    NODE_DIRS="$NODE_DIRS build/generated/node_${i}"
    i=$((i + 1))
  done

  # Wait for each validator dir to be fully populated. gen-autobahn-config
  # reads validator_pubkey, node_pubkey, autobahn_address, evmrpc_url; the
  # rpc container can be spawned in parallel with the cluster, so any of
  # these may not yet exist. Poll up to 5 minutes for evmrpc_url.txt (the
  # autobahn-specific file each validator step writes last).
  for d in $NODE_DIRS; do
    i=0
    while [ ! -f "$d/evmrpc_url.txt" ] && [ "$i" -lt 300 ]; do
      sleep 1
      i=$((i + 1))
    done
    if [ ! -f "$d/evmrpc_url.txt" ]; then
      echo "ERROR: $d/evmrpc_url.txt missing after 5 minutes; aborting." >&2
      exit 1
    fi
  done

  seid tendermint gen-autobahn-config $NODE_DIRS --output "$AUTOBAHN_CONFIG"

  # Inject autobahn-config-file as a top-level key in config.toml. It must
  # precede any [section] header so the TOML parser sees it at root scope.
  if grep -q "autobahn-config-file" ~/.sei/config/config.toml; then
    sed -i 's|autobahn-config-file = .*|autobahn-config-file = "'"$AUTOBAHN_CONFIG"'"|' ~/.sei/config/config.toml
  else
    sed -i '1s|^|autobahn-config-file = "'"$AUTOBAHN_CONFIG"'"\n|' ~/.sei/config/config.toml
  fi
  echo "Autobahn config written to $AUTOBAHN_CONFIG (fullnode via mode=full)"
fi

# Override state sync configs. Autobahn startup can legitimately sit at
# height 0 until a tx drives the first finalized block, so state sync
# cannot be configured yet in that case because trust-height 0 is invalid.
# Fall back to normal peer sync from genesis until the cluster has a real
# block history to trust.
STATE_SYNC_RPC="192.168.10.10:26657"
STATE_SYNC_PEER="2f9846450b7a3dcf4af1ac0082e3279c16744df8@172.31.9.18:26656,ec98c4a28a2023f4f976828c8a8e7127bfef4e1b@172.31.4.96:26656,b03014d67384fb0ef6ad992c77cefe4f9d2c1640@172.31.4.219:26656"
curl "$STATE_SYNC_RPC"/net_info |jq -r '.peers[] | .url' |sed -e 's#mconn://##' >> build/generated/PEERS
STATE_SYNC_PEER=$(paste -s -d ',' build/generated/PEERS)
LATEST_HEIGHT=$(curl -s $STATE_SYNC_RPC/block | jq -r .block.header.height)
if [ -n "$LATEST_HEIGHT" ] && [ "$LATEST_HEIGHT" -gt 0 ] 2>/dev/null; then
  SYNC_BLOCK_HEIGHT=$LATEST_HEIGHT
  SYNC_BLOCK_HASH=$(curl -s "$STATE_SYNC_RPC/block?height=$SYNC_BLOCK_HEIGHT" | jq -r .block_id.hash)
  sed -i.bak -e "s|^enable *=.*|enable = true|" ~/.sei/config/config.toml
  sed -i.bak -e "s|^rpc-servers *=.*|rpc-servers = \"$STATE_SYNC_RPC,$STATE_SYNC_RPC\"|" ~/.sei/config/config.toml
  sed -i.bak -e "s|^trust-height *=.*|trust-height = $SYNC_BLOCK_HEIGHT|" ~/.sei/config/config.toml
  sed -i.bak -e "s|^trust-hash *=.*|trust-hash = \"$SYNC_BLOCK_HASH\"|" ~/.sei/config/config.toml
  sed -i.bak -e "s|^persistent-peers *=.*|persistent-peers = \"$STATE_SYNC_PEER\"|" ~/.sei/config/config.toml
else
  echo "State sync unavailable at height ${LATEST_HEIGHT:-unknown}; falling back to peer sync."
  sed -i.bak -e "s|^enable *=.*|enable = false|" ~/.sei/config/config.toml
  sed -i.bak -e "s|^persistent-peers *=.*|persistent-peers = \"$STATE_SYNC_PEER\"|" ~/.sei/config/config.toml
fi
