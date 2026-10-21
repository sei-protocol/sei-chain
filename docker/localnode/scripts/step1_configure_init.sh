#!/usr/bin/env sh

# Input parameters
NODE_ID=${ID:-0}
NUM_ACCOUNTS=${NUM_ACCOUNTS:-5}
VALIDATOR=${VALIDATOR:-true}
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

# Initialize the node home. The topology selects validator or full-node mode
# after all nodes have contributed their shared genesis inputs.
MONIKER="sei-node-$NODE_ID"

seid init "$MONIKER" --chain-id sei >/dev/null 2>&1

# Copy configs
APP_CONFIG_FILE="$NODE_DIR/app.toml"
TENDERMINT_CONFIG_FILE="$NODE_DIR/config.toml"
cp docker/localnode/config/app.toml "$APP_CONFIG_FILE"
cp docker/localnode/config/config.toml "$TENDERMINT_CONFIG_FILE"


# Set up persistent peers
SEI_NODE_ID=$(seid tendermint show-node-id)
if [ -n "$ADVERTISE_IP" ]; then
  NODE_IP="$ADVERTISE_IP"
else
  NODE_IP=$(hostname -i | awk '{print $1}')
fi
P2P_PORT=26656  # Must match [p2p] laddr in config.toml
EVMWS_PORT=8546  # Must match the EVM RPC WebSocket port (evmrpc DefaultConfig WSPort).
echo "$SEI_NODE_ID@$NODE_IP:$P2P_PORT" >> build/generated/persistent_peers.txt

# Store autobahn-compatible pubkeys and address for config generation
cp ~/.sei/config/validator_pubkey.txt "$NODE_DIR/" || { echo "ERROR: failed to copy validator_pubkey.txt"; exit 1; }
cp ~/.sei/config/node_pubkey.txt "$NODE_DIR/" || { echo "ERROR: failed to copy node_pubkey.txt"; exit 1; }
echo "$NODE_IP:$P2P_PORT" > "$NODE_DIR/autobahn_address.txt"
echo "ws://$NODE_IP:$EVMWS_PORT" > "$NODE_DIR/evmrpc_url.txt"
# Autobahn EVM governance votes for node N with anvil account N of the test mnemonic.
if [ -n "$EVM_GOVERNANCE_VOTING_PERIOD" ]; then
  set -- 0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266 0x70997970C51812dc3A010C7d01b50e0d17dc79C8 \
    0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC 0x90F79bf6EB2c4f870365E785982E1f101E93b906 \
    0x15d34AAf54267DB7D7c367839AAf71A00a2C6A65 0x9965507D1a55bcC2695C58ba16FB37d819B0A4dc \
    0x976EA74026E726554dB657fA54763abd0C3a0aa9 0x14dC79964da2C08b23698B3D3cc7Ca32193d9955 \
    0x23618e81E3f5cdF7f54C3d65f7FBc0aBf5B21E8f 0xa0Ee7A142d267C1f36714E4a8F75612F20a79720
  shift "$NODE_ID"
  echo "$1" > "$NODE_DIR/evm_voter.txt"
fi

# Create a new account
ACCOUNT_NAME="node_admin"
echo "Adding account $ACCOUNT_NAME"
printf "12345678\n12345678\ny\n" | seid keys add "$ACCOUNT_NAME" >/dev/null 2>&1

# Get genesis account info
GENESIS_ACCOUNT_ADDRESS=$(printf "12345678\n" | seid keys show "$ACCOUNT_NAME" -a)
echo "$GENESIS_ACCOUNT_ADDRESS" >> build/generated/genesis_accounts.txt

# Add funds to genesis account
seid add-genesis-account "$GENESIS_ACCOUNT_ADDRESS" 10000000usei,10000000uusdc,10000000uatom

# Only validator nodes contribute a gentx to the genesis validator set. Full
# nodes still receive funded accounts and persistent-peer configuration.
if [ "$VALIDATOR" = "true" ]; then
  printf "12345678\n" | seid gentx "$ACCOUNT_NAME" 10000000usei --chain-id sei
  cp ~/.sei/config/gentx/* build/generated/gentx/
fi

# Creating some testing accounts
echo "Creating $NUM_ACCOUNTS accounts"
python3 scripts/populate_genesis_accounts.py "$NUM_ACCOUNTS" loc >/dev/null 2>&1
echo "Finished $NUM_ACCOUNTS accounts creation"

# Set node seivaloper info
SEIVALOPER_INFO=$(printf "12345678\n" | seid keys show "$ACCOUNT_NAME" --bech=val -a)
PRIV_KEY=$(printf "12345678\n12345678\n" | seid keys export "$ACCOUNT_NAME")
echo "$PRIV_KEY" >> build/generated/exported_keys/"$SEIVALOPER_INFO".txt

echo "DONE" >> build/generated/init.complete
