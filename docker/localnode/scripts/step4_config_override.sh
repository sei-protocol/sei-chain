#!/usr/bin/env sh

NODE_ID=${ID:-0}
GIGA_EXECUTOR=${GIGA_EXECUTOR:-false}
GIGA_OCC=${GIGA_OCC:-false}
AUTOBAHN=${AUTOBAHN:-false}
GIGA_STORAGE=${GIGA_STORAGE:-false}
# GIGA_FLATKV_ONLY=true boots the cluster directly in the terminal v3
# steady state: all SC writes route to FlatKV and memiavl is not allocated.
# This is intended for end-to-end state-sync coverage of the post-migration
# shape, not for exercising the migration itself.
GIGA_FLATKV_ONLY=${GIGA_FLATKV_ONLY:-false}
# GIGA_MIGRATE_FROM_MEMIAVL=true boots the cluster in v0 (memiavl_only):
# memiavl is the sole SC backend, FlatKV is not allocated. This is the
# starting point for the FlatKV EVM migrate cluster test, which drives a
# real workload in this mode and then performs a coordinated stop/flip/
# restart into migrate_evm. Mutually exclusive with GIGA_STORAGE=true;
# the script picks the more specific override if both are set.
GIGA_MIGRATE_FROM_MEMIAVL=${GIGA_MIGRATE_FROM_MEMIAVL:-false}

if [ "$GIGA_MIGRATE_FROM_MEMIAVL" = "true" ] && [ "$GIGA_FLATKV_ONLY" = "true" ]; then
  echo "GIGA_MIGRATE_FROM_MEMIAVL and GIGA_FLATKV_ONLY are mutually exclusive" >&2
  exit 1
fi

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

# pin_sc_write_mode MODE: set sc-write-mode = "MODE" and pin
# sc-write-mode-enable-auto = false so the explicit mode is actually honored.
# sc-write-mode-enable-auto defaults to true, which forces the node into auto
# and ignores any explicit sc-write-mode; these test clusters need the exact
# pinned backend, so they must opt out of auto.
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

# Boot the cluster in v0 (memiavl_only) for the FlatKV EVM migrate test.
# Doing this here keeps the override surface narrow: the test runner
# only has to set one env var to ship a v0-shaped config, and the
# follow-up flip script just rewrites sc-write-mode in place during the
# coordinated stop.
if [ "$GIGA_MIGRATE_FROM_MEMIAVL" = "true" ]; then
  echo "Booting node $NODE_ID in memiavl_only mode (FlatKV EVM migrate starting point)..."
  pin_sc_write_mode "memiavl_only"
  # The EVM SS split is irrelevant in this mode (flatkv is not allocated),
  # but explicitly disabling it keeps app.toml self-describing in case an
  # operator inspects it post-flip.
  sed -i 's/^evm-ss-split[[:space:]]*=.*/evm-ss-split = false/' ~/.sei/config/app.toml
fi

# Boot the cluster directly in v3 (flatkv_only) for post-migration state-sync
# coverage. This mode must not also run the GIGA_STORAGE dual-write override.
if [ "$GIGA_FLATKV_ONLY" = "true" ]; then
  echo "Booting node $NODE_ID in flatkv_only mode (post-migration steady state)..."
  pin_sc_write_mode "flatkv_only"
  sed -i 's/^evm-ss-split[[:space:]]*=.*/evm-ss-split = false/' ~/.sei/config/app.toml
fi

# Enable Giga Storage: FlatKV SC dual-write + EVM SS split.
# When GIGA_STORAGE=true we also default the receipt backend to pebble; callers
# can still override this by setting RECEIPT_BACKEND explicitly.
# Set GIGA_STORAGE=false to disable.
# GIGA_MIGRATE_FROM_MEMIAVL takes precedence: if both are set, the memiavl-only
# block above ran first and the test runner is responsible for the migration.
if [ "$GIGA_STORAGE" = "true" ] && [ "$GIGA_MIGRATE_FROM_MEMIAVL" != "true" ] && [ "$GIGA_FLATKV_ONLY" != "true" ]; then
  RECEIPT_BACKEND=${RECEIPT_BACKEND:-pebble}
  echo "Enabling Giga Storage for node $NODE_ID..."

  # --- SC layer: test_only_dual_write ---
  # SC must use test_only_dual_write because block execution reads EVM data
  # from the memiavl tree via GetChildStoreByName. dual-write keeps memiavl
  # up-to-date for reads while also populating FlatKV. This mode is for test
  # clusters only — never deploy to testnet/mainnet.
  pin_sc_write_mode "test_only_dual_write"

  # --- SS layer: enable EVM split ---
  sed -i 's/^evm-ss-split[[:space:]]*=.*/evm-ss-split = true/' ~/.sei/config/app.toml
fi

# Enable Giga Executor if requested
if [ "$GIGA_EXECUTOR" = "true" ]; then
  echo "Enabling Giga Executor for node $NODE_ID..."
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
