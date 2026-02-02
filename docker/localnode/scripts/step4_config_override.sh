#!/usr/bin/env sh

NODE_ID=${ID:-0}
GIGA_EXECUTOR=${GIGA_EXECUTOR:-false}
GIGA_OCC=${GIGA_OCC:-false}

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
