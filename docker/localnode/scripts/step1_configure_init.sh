#!/usr/bin/env sh

# Input parameters
NODE_ID=${ID:-0}
NUM_ACCOUNTS=${NUM_ACCOUNTS:-5}
echo "Configure and initialize environment"

cp build/seid "$GOBIN"/

# Prepare shared folders
NODE_DIR="build/generated/node_${NODE_ID}"
mkdir -p build/generated/gentx/
mkdir -p build/generated/exported_keys/
mkdir -p "$NODE_DIR"

# Testing whether seid works or not
seid version # Uncomment the below line if there are any dependency issues
# ldd build/seid

# Initialize validator node
MONIKER="sei-node-$NODE_ID"

seid init "$MONIKER" --chain-id sei >/dev/null 2>&1

# Copy configs
APP_CONFIG_FILE="$NODE_DIR/app.toml"
TENDERMINT_CONFIG_FILE="$NODE_DIR/config.toml"
cp docker/localnode/config/app.toml "$APP_CONFIG_FILE"
cp docker/localnode/config/config.toml "$TENDERMINT_CONFIG_FILE"


# Set up persistent peers
SEI_NODE_ID=$(seid tendermint show-node-id)
NODE_IP=$(hostname -i | awk '{print $1}')
P2P_PORT=26656  # Must match [p2p] laddr in config.toml
EVMRPC_PORT=8545  # Must match the EVM RPC HTTP port (evmrpc DefaultConfig HTTPPort).
echo "$SEI_NODE_ID@$NODE_IP:$P2P_PORT" >> build/generated/persistent_peers.txt

# Store autobahn-compatible pubkeys and address for config generation
cp ~/.sei/config/validator_pubkey.txt "$NODE_DIR/" || { echo "ERROR: failed to copy validator_pubkey.txt"; exit 1; }
cp ~/.sei/config/node_pubkey.txt "$NODE_DIR/" || { echo "ERROR: failed to copy node_pubkey.txt"; exit 1; }
echo "$NODE_IP:$P2P_PORT" > "$NODE_DIR/autobahn_address.txt"
echo "http://$NODE_IP:$EVMRPC_PORT" > "$NODE_DIR/evmrpc_url.txt"

# Create a new account
ACCOUNT_NAME="node_admin"
echo "Adding account $ACCOUNT_NAME"
printf "12345678\n12345678\ny\n" | seid keys add "$ACCOUNT_NAME" >/dev/null 2>&1

# Get genesis account info
GENESIS_ACCOUNT_ADDRESS=$(printf "12345678\n" | seid keys show "$ACCOUNT_NAME" -a)
echo "$GENESIS_ACCOUNT_ADDRESS" >> build/generated/genesis_accounts.txt

# Add funds to genesis account
seid add-genesis-account "$GENESIS_ACCOUNT_ADDRESS" 10000000usei,10000000uusdc,10000000uatom

# Create gentx
printf "12345678\n" | seid gentx "$ACCOUNT_NAME" 10000000usei --chain-id sei
cp ~/.sei/config/gentx/* build/generated/gentx/

# Creating some testing accounts
echo "Creating $NUM_ACCOUNTS accounts"
python3 loadtest/scripts/populate_genesis_accounts.py "$NUM_ACCOUNTS" loc >/dev/null 2>&1
echo "Finished $NUM_ACCOUNTS accounts creation"

# Set node seivaloper info
SEIVALOPER_INFO=$(printf "12345678\n" | seid keys show "$ACCOUNT_NAME" --bech=val -a)
PRIV_KEY=$(printf "12345678\n12345678\n" | seid keys export "$ACCOUNT_NAME")
echo "$PRIV_KEY" >> build/generated/exported_keys/"$SEIVALOPER_INFO".txt

echo "DONE" >> build/generated/init.complete
