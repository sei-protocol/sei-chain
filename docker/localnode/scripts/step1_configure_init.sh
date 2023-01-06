#!/usr/bin/env sh

# Input parameters
NODE_ID=${ID:-0}
NUM_ACCOUNTS=${NUM_ACCOUNTS:-5}
echo "Configure and initialize environment"
# Set up GO PATH
export GOPATH=$HOME/go
export GOBIN=$GOPATH/bin
export BUILD_PATH=/sei-protocol/sei-chain/build
export PATH=$GOBIN:$PATH:/usr/local/go/bin:$BUILD_PATH
mkdir -p $GOBIN
cp build/seid $GOBIN/

# Prepare shared folders
mkdir -p build/generated/gentx/
mkdir -p build/generated/exported_keys/
# Testing whether seid works or not
./build/seid version # Uncomment the below line if there are any dependency issues
# ldd build/seid

# Initialize validator node
MONIKER="sei-node-$NODE_ID"
./build/seid init "$MONIKER" --chain-id sei

# Copy configs
cp docker/localnode/config/app.toml ~/.sei/config/app.toml
cp docker/localnode/config/config.toml ~/.sei/config/config.toml
cp docker/localnode/config/price_feeder_config.toml ~/price_feeder_config.toml

# Set up persistent peers
SEI_NODE_ID=$(./build/seid tendermint show-node-id)
NODE_IP=$(hostname -i | awk '{print $1}')
echo "$SEI_NODE_ID@$NODE_IP:26656" >> build/generated/persistent_peers.txt

# Create a new account
ACCOUNT_NAME="node_admin"
echo "Adding account $ACCOUNT_NAME"
printf "12345678\n12345678\ny\n" | ./build/seid keys add "$ACCOUNT_NAME" >/dev/null 2>&1

# Get genesis account info
GENESIS_ACCOUNT_ADDRESS=$(printf "12345678\n" | ./build/seid keys show "$ACCOUNT_NAME" -a)
echo "$GENESIS_ACCOUNT_ADDRESS" >> build/generated/genesis_accounts.txt

# Add funds to genesis account
./build/seid add-genesis-account "$GENESIS_ACCOUNT_ADDRESS" 10000000usei

# Create gentx
printf "12345678\n" | ./build/seid gentx "$ACCOUNT_NAME" 10000000usei --chain-id sei
cp ~/.sei/config/gentx/* build/generated/gentx/

# Creating some testing accounts
echo "Creating $NUM_ACCOUNTS accounts"
python3 loadtest/scripts/populate_genesis_accounts.py $NUM_ACCOUNTS loc >/dev/null 2>&1
echo "Finished $NUM_ACCOUNTS accounts creation"

# Set node0 seivaloper info
NODE0_SEIVALOPER_INFO=$(printf "12345678\n" | ./build/seid keys show "$ACCOUNT_NAME" --bech=val -a)
PRIV_KEY=$(printf "12345678\n12345678\n" | ./build/seid keys export "$ACCOUNT_NAME")
echo "$PRIV_KEY" >> build/generated/exported_keys/"$NODE0_SEIVALOPER_INFO".txt

# Update price_feeder_config.toml with address info
sed -i'' -e 's/address = "sei"/address = \"'$GENESIS_ACCOUNT_ADDRESS'\"/g' ~/price_feeder_config.toml
sed -i'' -e 's/validator = "seivaloper"/validator = \"'$NODE0_SEIVALOPER_INFO'\"/g' ~/price_feeder_config.toml

echo "DONE" >> build/generated/init.complete
