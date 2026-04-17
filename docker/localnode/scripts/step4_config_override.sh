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

# Enable Giga Storage: FlatKV SC split + EVM SS split + parquet receipts.
# Set GIGA_STORAGE=false to disable.
if [ "$GIGA_STORAGE" = "true" ]; then
  echo "Enabling Giga Storage for node $NODE_ID..."

  # --- SC layer: split_write, split_read, lattice hash ---
  if grep -q "sc-write-mode" ~/.sei/config/app.toml; then
    sed -i 's/sc-write-mode = .*/sc-write-mode = "split_write"/' ~/.sei/config/app.toml
  else
    sed -i '/^\[state-store\]/i sc-write-mode = "split_write"' ~/.sei/config/app.toml
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

  # --- SS layer: EVM split_write + split_read ---
  sed -i 's/evm-ss-write-mode = .*/evm-ss-write-mode = "split_write"/' ~/.sei/config/app.toml
  sed -i 's/evm-ss-read-mode = .*/evm-ss-read-mode = "split_read"/' ~/.sei/config/app.toml

  # --- Receipt store: parquet backend ---
  if grep -q "\[receipt-store\]" ~/.sei/config/app.toml; then
    sed -i 's/rs-backend = .*/rs-backend = "parquet"/' ~/.sei/config/app.toml
  else
    echo "" >> ~/.sei/config/app.toml
    echo "[receipt-store]" >> ~/.sei/config/app.toml
    echo 'rs-backend = "parquet"' >> ~/.sei/config/app.toml
  fi
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
