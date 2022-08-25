KEY="mykey"
CHAINID="sei-local"
MONIKER="localtestnet"
KEYRING="test" # remember to change to other types of keyring like 'file' in-case exposing to outside world, otherwise your balance will be wiped quickly. The keyring test does not require private key to steal tokens from you
KEYALGO="secp256k1"
ETHKEYALGO="eth_secp256k1"
LOGLEVEL="info"
# to trace evm
#TRACE="--trace"
TRACE=""

# validate dependencies are installed
command -v jq > /dev/null 2>&1 || { echo >&2 "jq not installed. More info: https://stedolan.github.io/jq/download/"; exit 1; }

# used to exit on first error (any non-zero exit code)
set -e

# Clear everything of previous installation
rm -rf ~/.sei

# Reinstall daemon
make install

# Set client config
seid config keyring-backend $KEYRING
seid config chain-id $CHAINID

# if $KEY exists it should be deleted
seid keys add $KEY --keyring-backend $KEYRING --algo $ETHKEYALGO

# Set moniker and chain-id for Evmos (Moniker can be anything, chain-id must be an integer)
seid init $MONIKER --chain-id $CHAINID

# Change parameter token denominations to usei
cat $HOME/.sei/config/genesis.json | jq '.app_state["evm"]["params"]["evm_denom"]="usei"' > $HOME/.sei/config/tmp_genesis.json && mv $HOME/.sei/config/tmp_genesis.json $HOME/.sei/config/genesis.json
cat $HOME/.sei/config/genesis.json | jq '.app_state["gov"]["voting_params"]["voting_period"]="30s"' > $HOME/.sei/config/tmp_genesis.json && mv $HOME/.sei/config/tmp_genesis.json $HOME/.sei/config/genesis.json
cat $HOME/.sei/config/genesis.json | jq '.app_state["crisis"]["constant_fee"]["denom"]="usei"' > $HOME/.sei/config/tmp_genesis.json && mv $HOME/.sei/config/tmp_genesis.json $HOME/.sei/config/genesis.json
cat $HOME/.sei/config/genesis.json | jq '.app_state["gov"]["deposit_params"]["min_deposit"][0]["denom"]="usei"' > $HOME/.sei/config/tmp_genesis.json && mv $HOME/.sei/config/tmp_genesis.json $HOME/.sei/config/genesis.json
cat $HOME/.sei/config/genesis.json | jq '.app_state["mint"]["params"]["mint_denom"]="usei"' > $HOME/.sei/config/tmp_genesis.json && mv $HOME/.sei/config/tmp_genesis.json $HOME/.sei/config/genesis.json
cat $HOME/.sei/config/genesis.json | jq '.app_state["staking"]["params"]["bond_denom"]="usei"' > $HOME/.sei/config/tmp_genesis.json && mv $HOME/.sei/config/tmp_genesis.json $HOME/.sei/config/genesis.json


if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' 's/create_empty_blocks = true/create_empty_blocks = false/g' $HOME/.sei/config/config.toml
    sed -i '' 's/create_empty_blocks_interval = "0s"/create_empty_blocks_interval = "30s"/g' $HOME/.sei/config/config.toml
    sed -i '' 's/timeout_commit = "250ms"/timeout_commit = "1s"/g' $HOME/.sei/config/config.toml
    sed -i '' 's/skip_timeout_commit = true/skip_timeout_commit = false/g' $HOME/.sei/config/config.toml
    sed -i '' 's#laddr = "tcp://127.0.0.1:26657"# laddr = "tcp://0.0.0.0:26657"#g' $HOME/.sei/config/config.toml
else
    sed -i 's/create_empty_blocks = true/create_empty_blocks = false/g' $HOME/.sei/config/config.toml
    sed -i 's/create_empty_blocks_interval = "0s"/create_empty_blocks_interval = "30s"/g' $HOME/.sei/config/config.toml
    sed -i 's/timeout_commit = "250ms"/timeout_commit = "1s"/g' $HOME/.sei/config/config.toml
    sed -i 's/skip_timeout_commit = true/skip_timeout_commit = false/g' $HOME/.sei/config/config.toml
    sed -i 's#laddr = "tcp://127.0.0.1:26657"# laddr = "tcp://0.0.0.0:26657"#g' $HOME/.sei/config/config.toml

fi

# Allocate genesis accounts (cosmos formatted addresses)
seid add-genesis-account $KEY 100000000000000000000000000usei --keyring-backend $KEYRING

# Sign genesis transaction
seid gentx $KEY 1000000000000000000000usei --keyring-backend $KEYRING --chain-id $CHAINID
# Collect genesis tx
seid collect-gentxs

# Run this to ensure everything worked and that the genesis file is setup correctly
seid validate-genesis

if [[ $1 == "pending" ]]; then
  echo "pending mode is on, please wait for the first block committed."
fi

# Start the node (remove the --pruning=nothing flag if historical queries are not needed)
seid start $TRACE --log_level $LOGLEVEL --minimum-gas-prices=0.0001usei --json-rpc.api eth,txpool,personal,net,debug,web3
