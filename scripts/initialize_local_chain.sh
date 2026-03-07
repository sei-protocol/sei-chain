#!/bin/bash
# require success for commands
set -e

# Parse command line arguments
MOCK_BALANCES=${MOCK_BALANCES:-false}
GIGA_EXECUTOR=${GIGA_EXECUTOR:-false}
GIGA_OCC=${GIGA_OCC:-false}

# Use python3 as default, but fall back to python if python3 doesn't exist
PYTHON_CMD=python3
if ! command -v $PYTHON_CMD &> /dev/null
then
    PYTHON_CMD=python
fi

# set key name
keyname=admin
# Uncomment the following if you'd like to run jaeger
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
# Display configuration
echo "=== Local Chain Configuration ==="
echo "  MOCK_BALANCES:  $MOCK_BALANCES"
echo "  GIGA_EXECUTOR:  $GIGA_EXECUTOR"
echo "  GIGA_OCC:       $GIGA_OCC"
echo "================================="

# clean up old sei directory
rm -rf ~/.sei
echo "Building..."
# install seid -- conditionally build with mock balance function
if [ "$MOCK_BALANCES" = true ]; then
    echo "Building with mock balances enabled..."
    make install-mock-balances
else
    echo "Building with standard configuration..."
    make install
fi
# initialize chain with chain ID and add the first key
seid init demo --chain-id sei-chain --overwrite
seid keys add $keyname --keyring-backend test
# add the key as a genesis account with massive balances of several different tokens
seid add-genesis-account $(seid keys show $keyname -a --keyring-backend test) 100000000000000000000usei,100000000000000000000uusdc,100000000000000000000uatom --keyring-backend test
# gentx for account
seid gentx $keyname 7000000000000000usei --chain-id sei-chain --keyring-backend test
# add validator information to genesis file
KEY=$(jq '.pub_key' ~/.sei/config/priv_validator_key.json -c)
jq '.validators = [{}]' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
jq '.validators[0] += {"power":"7000000000"}' ~/.sei/config/tmp_genesis.json > ~/.sei/config/tmp_genesis_2.json
jq '.validators[0] += {"pub_key":'$KEY'}' ~/.sei/config/tmp_genesis_2.json > ~/.sei/config/tmp_genesis_3.json
mv ~/.sei/config/tmp_genesis_3.json ~/.sei/config/genesis.json && rm ~/.sei/config/tmp_genesis.json && rm ~/.sei/config/tmp_genesis_2.json

echo "Creating Accounts"
# create 10 test accounts + fund them
python3  loadtest/scripts/populate_genesis_accounts.py 20 loc

seid collect-gentxs
# update some params in genesis file for easier use of the chain localls (make gov props faster)
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["deposit_params"]["max_deposit_period"]="60s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["voting_params"]["voting_period"]="30s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["voting_params"]["expedited_voting_period"]="10s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["oracle"]["params"]["vote_period"]="2"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["oracle"]["params"]["whitelist"]=[{"name": "ueth"},{"name": "ubtc"},{"name": "uusdc"},{"name": "uusdt"},{"name": "uosmo"},{"name": "uatom"},{"name": "usei"}]' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["distribution"]["params"]["community_tax"]="0.000000000000000000"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.consensus_params["block"]["max_gas"]="35000000"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.consensus_params["block"]["min_txs_in_block"]="2"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.consensus_params["block"]["max_gas_wanted"]="50000000"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["staking"]["params"]["max_voting_power_ratio"]="1.000000000000000000"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["bank"]["denom_metadata"]=[{"denom_units":[{"denom":"usei","exponent":0,"aliases":["USEI"]}],"base":"usei","display":"usei","name":"USEI","symbol":"USEI"}]' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json

# Use the Python command to get the dates
START_DATE=$($PYTHON_CMD -c "from datetime import datetime; print(datetime.now().strftime('%Y-%m-%d'))")
END_DATE_3DAYS=$($PYTHON_CMD -c "from datetime import datetime, timedelta; print((datetime.now() + timedelta(days=3)).strftime('%Y-%m-%d'))")
END_DATE_5DAYS=$($PYTHON_CMD -c "from datetime import datetime, timedelta; print((datetime.now() + timedelta(days=5)).strftime('%Y-%m-%d'))")

cat ~/.sei/config/genesis.json | jq --arg start_date "$START_DATE" --arg end_date "$END_DATE_3DAYS" '.app_state["mint"]["params"]["token_release_schedule"]=[{"start_date": $start_date, "end_date": $end_date, "token_release_amount": "999999999999"}]' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq --arg start_date "$END_DATE_3DAYS" --arg end_date "$END_DATE_5DAYS" '.app_state["mint"]["params"]["token_release_schedule"] += [{"start_date": $start_date, "end_date": $end_date, "token_release_amount": "999999999999"}]' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json

if [ ! -z "$2" ]; then
  APP_TOML_PATH="$2"
else
  APP_TOML_PATH="$HOME/.sei/config/app.toml"
fi
# Enable OCC and SeiDB
sed -i.bak -e 's/# concurrency-workers = .*/concurrency-workers = 500/' $APP_TOML_PATH
sed -i.bak -e 's/occ-enabled = .*/occ-enabled = true/' $APP_TOML_PATH
sed -i.bak -e 's/sc-enable = .*/sc-enable = true/' $APP_TOML_PATH
sed -i.bak -e 's/ss-enable = .*/ss-enable = true/' $APP_TOML_PATH

# Enable Giga Executor (evmone-based) if requested
if [ "$GIGA_EXECUTOR" = true ]; then
  echo "Enabling Giga Executor (evmone-based EVM)..."
  if grep -q "\[giga_executor\]" $APP_TOML_PATH; then
    # If the section exists, update enabled to true
    if [[ "$OSTYPE" == "darwin"* ]]; then
      sed -i '' '/\[giga_executor\]/,/^\[/ s/enabled = false/enabled = true/' $APP_TOML_PATH
    else
      sed -i '/\[giga_executor\]/,/^\[/ s/enabled = false/enabled = true/' $APP_TOML_PATH
    fi
  else
    # If section doesn't exist, append it
    echo "" >> $APP_TOML_PATH
    echo "[giga_executor]" >> $APP_TOML_PATH
    echo "enabled = true" >> $APP_TOML_PATH
    echo "occ_enabled = false" >> $APP_TOML_PATH
  fi

  # Set OCC based on GIGA_OCC flag
  if [ "$GIGA_OCC" = true ]; then
    echo "Enabling OCC for Giga Executor..."
    if [[ "$OSTYPE" == "darwin"* ]]; then
      sed -i '' 's/occ_enabled = false/occ_enabled = true/' $APP_TOML_PATH
    else
      sed -i 's/occ_enabled = false/occ_enabled = true/' $APP_TOML_PATH
    fi
  else
    echo "Disabling OCC for Giga Executor (sequential mode)..."
    if [[ "$OSTYPE" == "darwin"* ]]; then
      sed -i '' 's/occ_enabled = true/occ_enabled = false/' $APP_TOML_PATH
    else
      sed -i 's/occ_enabled = true/occ_enabled = false/' $APP_TOML_PATH
    fi
  fi
fi

# set block time to 2s
if [ ! -z "$1" ]; then
  CONFIG_PATH="$1"
else
  CONFIG_PATH="$HOME/.sei/config/config.toml"
fi

if [ ! -z "$2" ]; then
  APP_PATH="$2"
else
  APP_PATH="$HOME/.sei/config/app.toml"
fi

if [[ "$OSTYPE" == "linux-gnu"* ]]; then
  sed -i 's/mode = "full"/mode = "validator"/g' $CONFIG_PATH
  sed -i 's/indexer = \["null"\]/indexer = \["kv"\]/g' $CONFIG_PATH
  sed -i 's/timeout_prevote =.*/timeout_prevote = "2000ms"/g' $CONFIG_PATH
  sed -i 's/timeout_precommit =.*/timeout_precommit = "2000ms"/g' $CONFIG_PATH
  sed -i 's/timeout_commit =.*/timeout_commit = "2000ms"/g' $CONFIG_PATH
  sed -i 's/skip_timeout_commit =.*/skip_timeout_commit = false/g' $CONFIG_PATH
  # sed -i 's/slow = false/slow = true/g' $APP_PATH
elif [[ "$OSTYPE" == "darwin"* ]]; then
  sed -i '' 's/mode = "full"/mode = "validator"/g' $CONFIG_PATH
  sed -i '' 's/indexer = \["null"\]/indexer = \["kv"\]/g' $CONFIG_PATH
  sed -i '' 's/unsafe-propose-timeout-override =.*/unsafe-propose-timeout-override = "2s"/g' $CONFIG_PATH
  sed -i '' 's/unsafe-propose-timeout-delta-override =.*/unsafe-propose-timeout-delta-override = "2s"/g' $CONFIG_PATH
  sed -i '' 's/unsafe-vote-timeout-override =.*/unsafe-vote-timeout-override = "2s"/g' $CONFIG_PATH
  sed -i '' 's/unsafe-vote-timeout-delta-override =.*/unsafe-vote-timeout-delta-override = "2s"/g' $CONFIG_PATH
  sed -i '' 's/unsafe-commit-timeout-override =.*/unsafe-commit-timeout-override = "2s"/g' $CONFIG_PATH
  # sed -i '' 's/slow = false/slow = true/g' $APP_PATH
else
  printf "Platform not supported, please ensure that the following values are set in your config.toml:\n"
  printf "###         Consensus Configuration Options         ###\n"
  printf "\t timeout_prevote = \"2000ms\"\n"
  printf "\t timeout_precommit = \"2000ms\"\n"
  printf "\t timeout_commit = \"2000ms\"\n"
  printf "\t skip_timeout_commit = false\n"
  exit 1
fi

seid config keyring-backend test

if [ $NO_RUN = 1 ]; then
  echo "No run flag set, exiting without starting the chain"
  exit 0
fi

# start the chain with log tracing
GORACE="log_path=/tmp/race/seid_race" seid start --trace --chain-id sei-chain
