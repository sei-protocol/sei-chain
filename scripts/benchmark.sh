#!/bin/bash
# require success for commands
set -e

# Parse command line arguments
MOCK_BALANCES=${MOCK_BALANCES:-true}
GIGA_EXECUTOR=${GIGA_EXECUTOR:-false}
GIGA_OCC=${GIGA_OCC:-false}
BENCHMARK_TXS_PER_BATCH=${BENCHMARK_TXS_PER_BATCH:-1000}
DISABLE_INDEXER=${DISABLE_INDEXER:-true}
# Debug mode - if true, prints all log output without filtering
DEBUG=${DEBUG:-false}
# Benchmark scenario config (path to JSON file, see scripts/scenarios/)
BENCHMARK_CONFIG=${BENCHMARK_CONFIG:-"scripts/scenarios/evm.json"}

# DB_BACKEND options:
#   goleveldb - default, pure Go, can have compaction stalls under heavy write load
#   memdb     - in-memory only, fastest (no disk I/O), data lost on restart
#   cleveldb  - C LevelDB, faster than goleveldb, requires CGO
#   rocksdb   - best compaction control, requires CGO and rocksdb libs
DB_BACKEND=${DB_BACKEND:-goleveldb}

# Use python3 as default, but fall back to python if python3 doesn't exist
PYTHON_CMD=python3
if ! command -v $PYTHON_CMD &> /dev/null
then
    PYTHON_CMD=python
fi

# set key name
keyname=admin

# Display configuration
echo "=== Benchmark Configuration ==="
echo "  MOCK_BALANCES:           $MOCK_BALANCES"
echo "  GIGA_EXECUTOR:           $GIGA_EXECUTOR"
echo "  GIGA_OCC:                $GIGA_OCC"
echo "  DB_BACKEND:              $DB_BACKEND"
echo "  BENCHMARK_TXS_PER_BATCH: $BENCHMARK_TXS_PER_BATCH"
echo "  DISABLE_INDEXER:         $DISABLE_INDEXER"
echo "  DEBUG:                   $DEBUG"
echo "  BENCHMARK_CONFIG:        ${BENCHMARK_CONFIG:-(default: EVMTransfer)}"
echo ""
echo "Available scenarios in scripts/scenarios/:"
ls -1 scripts/scenarios/*.json 2>/dev/null | sed 's/^/    /' || echo "    (none found)"
echo "================================"

# clean up old sei directory
rm -rf ~/.sei
echo "Building..."

# Determine build options based on DB_BACKEND
BUILD_TAGS=""
case "$DB_BACKEND" in
  cleveldb)
    echo "Building with cleveldb support (C LevelDB - faster)..."
    BUILD_TAGS="cleveldb"
    ;;
  rocksdb)
    echo "Building with rocksdb support (best compaction control)..."
    BUILD_TAGS="rocksdb"
    ;;
  goleveldb|memdb)
    echo "Building with default goleveldb support..."
    ;;
  *)
    echo "ERROR: Unknown DB_BACKEND '$DB_BACKEND'. Valid options: goleveldb, memdb, cleveldb, rocksdb"
    exit 1
    ;;
esac

# install seid with benchmark support (includes mock_balances)
echo "Building with benchmark and mock balances support enabled..."
if [ -n "$BUILD_TAGS" ]; then
  COSMOS_BUILD_OPTIONS="$BUILD_TAGS" make install-bench
else
  make install-bench
fi
# initialize chain with chain ID and add the first key
~/go/bin/seid init demo --chain-id sei-chain --overwrite
~/go/bin/seid keys add $keyname --keyring-backend test
# add the key as a genesis account with massive balances of several different tokens
~/go/bin/seid add-genesis-account $(~/go/bin/seid keys show $keyname -a --keyring-backend test) 100000000000000000000usei,100000000000000000000uusdc,100000000000000000000uatom --keyring-backend test
# gentx for account
~/go/bin/seid gentx $keyname 7000000000000000usei --chain-id sei-chain --keyring-backend test
# add validator information to genesis file
KEY=$(jq '.pub_key' ~/.sei/config/priv_validator_key.json -c)
jq '.validators = [{}]' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
jq '.validators[0] += {"power":"7000000000"}' ~/.sei/config/tmp_genesis.json > ~/.sei/config/tmp_genesis_2.json
jq '.validators[0] += {"pub_key":'$KEY'}' ~/.sei/config/tmp_genesis_2.json > ~/.sei/config/tmp_genesis_3.json
mv ~/.sei/config/tmp_genesis_3.json ~/.sei/config/genesis.json && rm ~/.sei/config/tmp_genesis.json && rm ~/.sei/config/tmp_genesis_2.json

echo "Creating Accounts"
# create 10 test accounts + fund them
python3  loadtest/scripts/populate_genesis_accounts.py 20 loc

~/go/bin/seid collect-gentxs
# update some params in genesis file for easier use of the chain localls (make gov props faster)
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["deposit_params"]["max_deposit_period"]="60s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["voting_params"]["voting_period"]="30s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["voting_params"]["expedited_voting_period"]="10s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["oracle"]["params"]["vote_period"]="2"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["evm"]["params"]["target_gas_used_per_block"]="1000000000000"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["oracle"]["params"]["whitelist"]=[{"name": "ueth"},{"name": "ubtc"},{"name": "uusdc"},{"name": "uusdt"},{"name": "uosmo"},{"name": "uatom"},{"name": "usei"}]' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["distribution"]["params"]["community_tax"]="0.000000000000000000"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.consensus_params["block"]["max_gas"]="100000000"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.consensus_params["block"]["min_txs_in_block"]="2"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.consensus_params["block"]["max_gas_wanted"]="150000000"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
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
  if [ "$DISABLE_INDEXER" = true ]; then
    sed -i 's/indexer = \["kv"\]/indexer = \["null"\]/g' $CONFIG_PATH
    echo "Indexer disabled"
  fi
  sed -i 's/skip_timeout_commit =.*/skip_timeout_commit = false/g' $CONFIG_PATH
  sed -i 's/pprof-laddr = ""/pprof-laddr = ":6060"/g' $CONFIG_PATH
  # Set the DB backend
  sed -i "s/db-backend = \"goleveldb\"/db-backend = \"$DB_BACKEND\"/g" $CONFIG_PATH
  echo "DB backend set to: $DB_BACKEND"
elif [[ "$OSTYPE" == "darwin"* ]]; then
  sed -i '' 's/mode = "full"/mode = "validator"/g' $CONFIG_PATH
  if [ "$DISABLE_INDEXER" = true ]; then
    sed -i '' 's/indexer = \["kv"\]/indexer = \["null"\]/g' $CONFIG_PATH
    echo "Indexer disabled"
  fi
  sed -i '' 's/pprof-laddr = ""/pprof-laddr = ":6060"/g' $CONFIG_PATH
  # Set the DB backend
  sed -i '' "s/db-backend = \"goleveldb\"/db-backend = \"$DB_BACKEND\"/g" $CONFIG_PATH
  echo "DB backend set to: $DB_BACKEND"
else
  printf "Platform not supported, please ensure that the following values are set in your config.toml:\n"
  printf "###         Consensus Configuration Options         ###\n"
  printf "\t timeout_prevote = \"2000ms\"\n"
  printf "\t timeout_precommit = \"2000ms\"\n"
  printf "\t timeout_commit = \"2000ms\"\n"
  printf "\t skip_timeout_commit = false\n"
  exit 1
fi

~/go/bin/seid config keyring-backend test

# start the chain with log tracing
# Benchmark mode is enabled via build tag, no --benchmark flag needed
echo ""
echo "=== pprof enabled at http://localhost:6060/debug/pprof ==="
echo "To capture 30s CPU profile during benchmark:"
echo "  go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30"
echo "To capture heap profile:"
echo "  go tool pprof http://localhost:6060/debug/pprof/heap"
echo "============================================================"
echo ""
if [ "$DEBUG" = true ]; then
  # Debug mode: print all output
  BENCHMARK_CONFIG=$BENCHMARK_CONFIG BENCHMARK_TXS_PER_BATCH=$BENCHMARK_TXS_PER_BATCH ~/go/bin/seid start --chain-id sei-chain
else
  # Normal mode: filter to benchmark-related output only
  BENCHMARK_CONFIG=$BENCHMARK_CONFIG BENCHMARK_TXS_PER_BATCH=$BENCHMARK_TXS_PER_BATCH ~/go/bin/seid start --chain-id sei-chain 2>&1 | grep -E "(benchmark|Benchmark|deployed|transitioning)"
fi
