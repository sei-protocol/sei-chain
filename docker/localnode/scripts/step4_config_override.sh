#!/usr/bin/env sh

NODE_ID=${ID:-0}
GIGA_EXECUTOR=${GIGA_EXECUTOR:-false}
GIGA_OCC=${GIGA_OCC:-false}
AUTOBAHN=${AUTOBAHN:-false}
GIGA_STORAGE=${GIGA_STORAGE:-false}

APP_CONFIG_FILE="build/generated/node_$NODE_ID/app.toml"
TENDERMINT_CONFIG_FILE="build/generated/node_$NODE_ID/config.toml"
cp build/generated/genesis.json ~/.sei/config/genesis.json
cp "$APP_CONFIG_FILE" ~/.sei/config/app.toml
cp "$TENDERMINT_CONFIG_FILE" ~/.sei/config/config.toml

# Override up persistent peers
NODE_IP=$(hostname -i | awk '{print $1}')
PEERS=$(cat build/generated/persistent_peers.txt |grep -v "$NODE_IP" | paste -sd "," -)
sed -i'' -e 's/persistent-peers = ""/persistent-peers = "'$PEERS'"/g' ~/.sei/config/config.toml

# Override snapshot directory
sed -i.bak -e "s|^snapshot-directory *=.*|snapshot-directory = \"./build/generated/node_$NODE_ID/snapshots\"|" ~/.sei/config/app.toml

# Enable slow mode
sed -i.bak -e 's/slow = .*/slow = true/' ~/.sei/config/app.toml

# Enable Giga Storage: FlatKV SC dual-write + EVM SS split.
# When GIGA_STORAGE=true we also default the receipt backend to parquet; callers
# can still override this by setting RECEIPT_BACKEND explicitly.
# Set GIGA_STORAGE=false to disable.
if [ "$GIGA_STORAGE" = "true" ]; then
  RECEIPT_BACKEND=${RECEIPT_BACKEND:-parquet}
  echo "Enabling Giga Storage for node $NODE_ID..."

  # --- SC layer: test_only_dual_write ---
  # SC must use test_only_dual_write because block execution reads EVM data
  # from the memiavl tree via GetChildStoreByName. dual-write keeps memiavl
  # up-to-date for reads while also populating FlatKV. This mode is for test
  # clusters only — never deploy to testnet/mainnet.
  if grep -q "sc-write-mode" ~/.sei/config/app.toml; then
    sed -i 's/sc-write-mode = .*/sc-write-mode = "test_only_dual_write"/' ~/.sei/config/app.toml
  else
    sed -i '/^\[state-store\]/i sc-write-mode = "test_only_dual_write"' ~/.sei/config/app.toml
  fi

  # --- SS layer: enable EVM split ---
  sed -i 's/evm-ss-split = .*/evm-ss-split = true/' ~/.sei/config/app.toml
fi

# Enable Giga Executor (evmone-based) if requested
if [ "$GIGA_EXECUTOR" = "true" ]; then
  echo "Enabling Giga Executor (evmone-based EVM) for node $NODE_ID..."
  if grep -q "\[giga_executor\]" ~/.sei/config/app.toml; then
    # If the section exists, update enabled to true
    sed -i 's/enabled = false/enabled = true/' ~/.sei/config/app.toml
  else
    # If section doesn't exist, append it
    echo "" >> ~/.sei/config/app.toml
    echo "[giga_executor]" >> ~/.sei/config/app.toml
    echo "enabled = true" >> ~/.sei/config/app.toml
    echo "occ_enabled = false" >> ~/.sei/config/app.toml
  fi

  # Set OCC based on GIGA_OCC flag
  if [ "$GIGA_OCC" = "true" ]; then
    echo "Enabling OCC for Giga Executor on node $NODE_ID..."
    sed -i 's/occ_enabled = false/occ_enabled = true/' ~/.sei/config/app.toml
  else
    echo "Disabling OCC for Giga Executor (sequential mode) on node $NODE_ID..."
    sed -i 's/occ_enabled = true/occ_enabled = false/' ~/.sei/config/app.toml
  fi
fi

# Override receipt store backend if requested
RECEIPT_BACKEND=${RECEIPT_BACKEND:-}
if [ -n "$RECEIPT_BACKEND" ]; then
  echo "Setting receipt store backend to '$RECEIPT_BACKEND' for node $NODE_ID..."
  if grep -q "\[receipt-store\]" ~/.sei/config/app.toml; then
    # Section exists, update the backend value
    sed -i "s/rs-backend = .*/rs-backend = \"$RECEIPT_BACKEND\"/" ~/.sei/config/app.toml
  else
    # Section doesn't exist, append it
    echo "" >> ~/.sei/config/app.toml
    echo "[receipt-store]" >> ~/.sei/config/app.toml
    echo "rs-backend = \"$RECEIPT_BACKEND\"" >> ~/.sei/config/app.toml
  fi
fi

# Generate Autobahn (GigaRouter) config if requested
if [ "$AUTOBAHN" = "true" ]; then
  echo "Generating Autobahn config for node $NODE_ID..."
  AUTOBAHN_CONFIG="$HOME/.sei/config/autobahn.json"

  # Collect node directories as arguments
  NODE_DIRS=""
  for i in $(seq 0 $((CLUSTER_SIZE - 1))); do
    NODE_DIRS="$NODE_DIRS build/generated/node_${i}"
  done

  # Generate autobahn config using seid
  seid tendermint gen-autobahn-config $NODE_DIRS --output "$AUTOBAHN_CONFIG"

  # Inject autobahn config file path into config.toml
  # Must be placed before any [section] header so TOML parser reads it as a top-level key.
  if grep -q "autobahn-config-file" ~/.sei/config/config.toml; then
    sed -i 's|autobahn-config-file = .*|autobahn-config-file = "'"$AUTOBAHN_CONFIG"'"|' ~/.sei/config/config.toml
  else
    sed -i '1s|^|autobahn-config-file = "'"$AUTOBAHN_CONFIG"'"\n|' ~/.sei/config/config.toml
  fi
  echo "Autobahn config written to $AUTOBAHN_CONFIG"
fi
