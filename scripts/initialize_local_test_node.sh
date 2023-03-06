#!/bin/bash

echo -n Key Name to Add:
read keyname
echo
echo -n Release to Build \(please find the latest release on https://github.com/sei-protocol/sei-chain/releases. Example: 1.1.0beta\):
read release
echo

echo "Building..."
# git fetch --tags -f
# git checkout $release
# make install
# git checkout master
sudo -S rm -r ~/.sei/
sudo -S rm -r ~/test_accounts/
~/go/bin/seid tendermint unsafe-reset-all
~/go/bin/seid init demo --chain-id sei-chain

~/go/bin/seid keys add $keyname
~/go/bin/seid add-genesis-account $(~/go/bin/seid keys show $keyname -a) 100000000000000000000usei,100000000000000000000uusdc,100000000000000000000uatom
~/go/bin/seid gentx $keyname 70000000000000000000usei --chain-id sei-chain
KEY=$(jq '.pub_key' ~/.sei/config/priv_validator_key.json -c)
jq '.validators = [{}]' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
jq '.validators[0] += {"power":"70000000000000"}' ~/.sei/config/tmp_genesis.json > ~/.sei/config/tmp_genesis_2.json
jq '.validators[0] += {"pub_key":'$KEY'}' ~/.sei/config/tmp_genesis_2.json > ~/.sei/config/tmp_genesis_3.json
mv ~/.sei/config/tmp_genesis_3.json ~/.sei/config/genesis.json && rm ~/.sei/config/tmp_genesis.json && rm ~/.sei/config/tmp_genesis_2.json
~/go/bin/seid collect-gentxs
cat ~/.sei/config/genesis.json | jq '.app_state["crisis"]["constant_fee"]["denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["deposit_params"]["min_deposit"][0]["denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["mint"]["params"]["mint_denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["staking"]["params"]["bond_denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["deposit_params"]["max_deposit_period"]="300s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["voting_params"]["voting_period"]="5s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["distribution"]["params"]["community_tax"]="0.000000000000000000"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json

# set block time to 2s
if [ ! -z "$1" ]; then
  CONFIG_PATH="$1"
else
  CONFIG_PATH="$HOME/.sei/config/config.toml"
fi

if [[ "$OSTYPE" == "linux-gnu"* ]]; then
  sed -i 's/unsafe-propose-timeout-override =.*/unsafe-propose-timeout-override = "2000ms"/g' $CONFIG_PATH
  sed -i 's/unsafe-vote-timeout-override =.*/unsafe-vote-timeout-override = "2000ms"/g' $CONFIG_PATH
  sed -i 's/unsafe-commit-timeout-override =.*/unsafe-commit-timeout-override = "2000ms"/g' $CONFIG_PATH
  sed -i 's/unsafe-bypass-commit-timeout-override =.*/unsafe-bypass-commit-timeout-override = false/g' $CONFIG_PATH
  sed -i 's/mode = "full"/mode = "validator"/g' $HOME/.sei/config/config.toml
  sed -i 's/indexer = \["null"\]/indexer = \["kv"\]/g' $HOME/.sei/config/config.toml
elif [[ "$OSTYPE" == "darwin"* ]]; then
  printf "Platform darwin\n\n\n"
  sed -i '' 's/unsafe-propose-timeout-override =.*/unsafe-propose-timeout-override = "2000ms"/g' $CONFIG_PATH
  sed -i '' 's/unsafe-vote-timeout-override =.*/unsafe-vote-timeout-override = "2000ms"/g' $CONFIG_PATH
  sed -i '' 's/unsafe-commit-timeout-override =.*/unsafe-commit-timeout-override = "2000ms"/g' $CONFIG_PATH
  sed -i '' 's/unsafe-bypass-commit-timeout-override =.*/unsafe-bypass-commit-timeout-override = false/g' $CONFIG_PATH
  sed -i '' 's/mode = "full"/mode = "validator"/g' $HOME/.sei/config/config.toml
  sed -i '' 's/indexer = \["null"\]/indexer = \["kv"\]/g' $HOME/.sei/config/config.toml
else
  printf "Platform not supported, please ensure that the following values are set in your config.toml:\n"
  printf "###         Consensus Configuration Options         ###\n"
  printf "\t unsafe-propose-timeout-override = \"2000ms\"\n"
  printf "\t unsafe-vote-timeout-override = \"2000ms\"\n"
  printf "\t unsafe-commit-timeout-override = \"2000ms\"\n"
  printf "\t unsafe-bypass-commit-timeout-override = false\n"
  printf "\t mode = validator\n"
  printf "\t indexer = [\"kv\"]\n"
  exit 1
fi

# start the chain with log tracing
~/go/bin/seid start --trace --chain-id sei-chain
