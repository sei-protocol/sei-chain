#!/bin/bash
set -euo pipefail

# fail prints an error and exits.
fail() {
  echo "ERROR: $*" >&2
  exit 1
}

# rpc_field prints the value that the jq filter $2 selects from the result of
# GET $STATE_SYNC_RPC/$1, which Sei's RPC returns bare rather than inside a
# JSON-RPC envelope. It exits if the result holds no such value.
rpc_field() {
  local response value
  response=$(curl -fsS "$STATE_SYNC_RPC/$1") || fail "could not query $STATE_SYNC_RPC/$1"
  value=$(jq -r "$2 // empty" <<<"$response" || true)
  [ -n "$value" ] || fail "$STATE_SYNC_RPC/$1 returned no $2: ${response:0:300}"
  printf '%s\n' "$value"
}

# set_config_key sets key $2 to value $3 in the [$1] table of config.toml, or
# exits if it cannot.
set_config_key() {
  local table="/^\[$1\]/,/^\[/"
  sed -i.bak "$table s|^$2 *=.*|$2 = $3|" "$CONFIG"
  grep -qxF "$2 = $3" <<<"$(sed -n "$table p" "$CONFIG")" || fail "could not set $2 in the [$1] table of $CONFIG"
}

# Prompt for the State Sync RPC Endpoint and store it in STATE_SYNC_RPC
echo -n State Sync RPC Endpoint:
read -r STATE_SYNC_RPC
echo

# Prompt for the State Sync Peer and store it in STATE_SYNC_PEER
echo -n State Sync Peer:
read -r STATE_SYNC_PEER
echo

# The trust height and hash are resolved before the node is touched, so that a
# bad RPC response stops the script while the node is still intact.

# Fetch the latest block height from the State Sync RPC endpoint
LATEST_HEIGHT=$(rpc_field block .block.header.height)
# Checked before the arithmetic below, which evaluates any other string as an expression.
[[ $LATEST_HEIGHT =~ ^[1-9][0-9]*$ ]] || fail "latest block height $LATEST_HEIGHT is not a positive integer"
# Calculate the trust height (rounded down to the nearest 100,000)
BLOCK_HEIGHT=$(( (LATEST_HEIGHT / 100000) * 100000 ))
[ "$BLOCK_HEIGHT" -gt 0 ] || fail "latest block height $LATEST_HEIGHT rounds down to a trust height of 0"
# Fetch the block hash at the trust height
TRUST_HASH=$(rpc_field "block?height=$BLOCK_HEIGHT" .block_id.hash)
[[ $TRUST_HASH =~ ^[0-9A-Fa-f]{64}$ ]] || fail "block hash $TRUST_HASH at height $BLOCK_HEIGHT is not a 64-digit hex hash"
echo "State sync trust height: $BLOCK_HEIGHT, trust hash: $TRUST_HASH"

# Create a backup directory for keys
mkdir -p "$HOME/key_backup"

# Backup the validator key, node key, and state files
cp "$HOME/.sei/config/priv_validator_key.json" "$HOME/key_backup"
cp "$HOME/.sei/config/node_key.json" "$HOME/key_backup"
cp "$HOME/.sei/data/priv_validator_state.json" "$HOME/key_backup"

# Create a backup directory for the entire .sei configuration
mkdir -p "$HOME/.sei_backup"

# Move existing config, data, and wasm directories to the backup directory
mv "$HOME/.sei/config" "$HOME/.sei_backup"
mv "$HOME/.sei/data" "$HOME/.sei_backup"
# A node that has never started has no wasm directory yet.
if [ -d "$HOME/.sei/wasm" ]; then mv "$HOME/.sei/wasm" "$HOME/.sei_backup"; fi

# Remove everything else in the home directory except cosmovisor
find "$HOME/.sei" -mindepth 1 -maxdepth 1 ! -name cosmovisor -exec rm -rf {} +

# Restore the config directory, which holds the validator and node keys, and the
# validator state file from the backups
cp -r "$HOME/.sei_backup/config" "$HOME/.sei/"
mkdir -p "$HOME/.sei/data"
cp "$HOME/key_backup/priv_validator_state.json" "$HOME/.sei/data/"

# Set up /tmp as a 12G RAM disk to allow for more than 400 state sync chunks
sudo umount -l /tmp && sudo mount -t tmpfs -o size=12G,mode=1777 overflow /tmp

# Update the config.toml file to enable state sync with the appropriate settings
CONFIG="$HOME/.sei/config/config.toml"
set_config_key statesync enable true
set_config_key statesync rpc-servers "\"$STATE_SYNC_RPC,$STATE_SYNC_RPC\""
set_config_key statesync trust-height "$BLOCK_HEIGHT"
set_config_key statesync trust-hash "\"$TRUST_HASH\""

# Set the persistent peers in the config.toml file to the specified State Sync Peer
set_config_key p2p persistent-peers "\"$STATE_SYNC_PEER\""
