

#!/bin/bash

keyname=admin
#docker stop jaeger
#docker rm jaeger
#docker run -d --name jaeger \
#  -e COLLECTOR_ZIPKIN_HOST_PORT=:9411 \
#  -p 5775:5775/udp \
#  -p 6831:6831/udp \
#  -p 6832:6832/udp \
#  -p 5778:5778 \
#  -p 16686:16686 \
#  -p 14250:14250 \
#  -p 14268:14268 \
#  -p 14269:14269 \
#  -p 9411:9411 \
#  jaegertracing/all-in-one:1.33

rm -rf ~/.sei
echo "Building..."
make install
#echo $password | sudo -S rm -r ~/.sei/
#echo $password | sudo -S rm -r ~/test_accounts/
~/go/bin/seid init demo --chain-id sei-chain
~/go/bin/seid keys add $keyname --keyring-backend test
#yes | ~/go/bin/seid keys add faucet
~/go/bin/seid add-genesis-account $(~/go/bin/seid keys show $keyname -a --keyring-backend test) 100000000000000000000usei,100000000000000000000uusdc,100000000000000000000uatom
~/go/bin/seid gentx $keyname 70000000000000000000usei --chain-id sei-chain --keyring-backend test
sed -i '' 's/mode = "full"/mode = "validator"/g' $HOME/.sei/config/config.toml
sed -i '' 's/indexer = \["null"\]/indexer = \["kv"\]/g' $HOME/.sei/config/config.toml
KEY=$(jq '.pub_key' ~/.sei/config/priv_validator_key.json -c)
jq '.validators = [{}]' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
jq '.validators[0] += {"power":"70000000000000"}' ~/.sei/config/tmp_genesis.json > ~/.sei/config/tmp_genesis_2.json
jq '.validators[0] += {"pub_key":'$KEY'}' ~/.sei/config/tmp_genesis_2.json > ~/.sei/config/tmp_genesis_3.json
mv ~/.sei/config/tmp_genesis_3.json ~/.sei/config/genesis.json && rm ~/.sei/config/tmp_genesis.json && rm ~/.sei/config/tmp_genesis_2.json

echo "Creating Accounts"
python3  loadtest/scripts/populate_genesis_accounts.py 50 loc

~/go/bin/seid collect-gentxs
cat ~/.sei/config/genesis.json | jq '.app_state["crisis"]["constant_fee"]["denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["deposit_params"]["min_deposit"][0]["denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["mint"]["params"]["mint_denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["staking"]["params"]["bond_denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["deposit_params"]["max_deposit_period"]="300s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["voting_params"]["voting_period"]="5s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.consensus_params["block"]["time_iota_ms"]="50"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["distribution"]["params"]["community_tax"]="0.000000000000000000"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json

# set block time to 2s
if [ ! -z "$1" ]; then
  CONFIG_PATH="$1"
else
  CONFIG_PATH="$HOME/.sei/config/config.toml"
fi

if [[ "$OSTYPE" == "linux-gnu"* ]]; then
  sed -i 's/timeout_prevote =.*/timeout_prevote = "2000ms"/g' $CONFIG_PATH
  sed -i 's/timeout_precommit =.*/timeout_precommit = "2000ms"/g' $CONFIG_PATH
  sed -i 's/timeout_commit =.*/timeout_commit = "2000ms"/g' $CONFIG_PATH
  sed -i 's/skip_timeout_commit =.*/skip_timeout_commit = false/g' $CONFIG_PATH
elif [[ "$OSTYPE" == "darwin"* ]]; then
  sed -i '' 's/timeout_prevote =.*/timeout_prevote = "2000ms"/g' $CONFIG_PATH
  sed -i '' 's/timeout_precommit =.*/timeout_precommit = "2000ms"/g' $CONFIG_PATH
  sed -i '' 's/timeout_commit =.*/timeout_commit = "2000ms"/g' $CONFIG_PATH
  sed -i '' 's/skip_timeout_commit =.*/skip_timeout_commit = false/g' $CONFIG_PATH
else
  printf "Platform not supported, please ensure that the following values are set in your config.toml:\n"
  printf "###         Consensus Configuration Options         ###\n"
  printf "\t timeout_prevote = \"2000ms\"\n"
  printf "\t timeout_precommit = \"2000ms\"\n"
  printf "\t timeout_commit = \"2000ms\"\n"
  printf "\t skip_timeout_commit = false\n"
  exit 1
fi

# start the chain with log tracing
GORACE="log_path=/tmp/race/seid_race" ~/go/bin/seid start --trace --chain-id sei-chain
