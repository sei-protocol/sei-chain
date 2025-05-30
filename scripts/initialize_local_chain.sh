#!/bin/bash
# Enhanced local testnet setup script
# Features:
#   - Single-node or multi-node Docker-based testnet
#   - Customizable genesis parameters through interactive menu
#   - Optional oracle requirement for validators
#   - Test wallet with fixed mnemonic for easy testing
#   - Comprehensive cleanup and reset options
#   - tmux session for monitoring multiple nodes
#
# Usage:
#   ./initialize_local_chain.sh           # Interactive mode
#   ./initialize_local_chain.sh --cleanup # Clean previous deployments

set -e

#──────────────────────────────────────────────────────────────#
#                     Environment Variables                    #
#──────────────────────────────────────────────────────────────#
export GOCACHE=${GOCACHE:="/tmp/go-cache"}
export USERID=${USERID:=$(id -u)}
export GROUPID=${GROUPID:=$(id -g)}
export HOME_DIR=${HOME_DIR:=$HOME}

#──────────────────────────────────────────────────────────────#
#                       Color Codes & Logging                  #
#──────────────────────────────────────────────────────────────#
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'  # No Color

log() {
  echo -e "${GREEN}[$(date '+%Y-%m-%d %H:%M:%S')]${NC} $1"
}

warn() {
  echo -e "${YELLOW}[$(date '+%Y-%m-%d %H:%M:%S')] WARNING:${NC} $1"
}

error() {
  echo -e "${RED}[$(date '+%Y-%m-%d %H:%M:%S')] ERROR:${NC} $1"
  exit 1
}

#──────────────────────────────────────────────────────────────#
#                         Global Defaults                      #
#──────────────────────────────────────────────────────────────#
PROJECT_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
DISABLE_ORACLE=false
MULTI_NODE=false
NO_RUN=0
NODE_COUNT=4
CUSTOM_CONFIG=false
TMUX_AVAILABLE=false
TMUX_SESSION="sei_cluster_logs"
keyname=admin
PYTHON_CMD=python3
RESUME=false

# Fixed test wallet mnemonic - used in all modes
FIXED_MNEMONIC="weasel august square crack throw second polar athlete sunset expire small fossil shoot soul hip final system sudden artwork fire canyon call elite other"

# Default genesis parameters
MAX_GAS="35000000"
MIN_TXS="2"
PROPOSE_TIMEOUT="1000"
VOTE_TIMEOUT="50"
COMMIT_TIMEOUT="50"
VOTING_PERIOD="30"
EXPEDITED_VOTING_PERIOD="10"
MAX_DEPOSIT_PERIOD="60"
UNBONDING_TIME="1814400"
MAX_VALIDATORS="35"
MIN_COMMISSION_RATE="0.05"
VOTE_PERIOD="2"
VOTE_THRESHOLD="0.667"
MIN_VALID_PER_WINDOW="0.05"
MAX_VOTING_POWER_RATIO="0.200000000000000000"

# Initialize Python command
if ! command -v $PYTHON_CMD &> /dev/null; then
  PYTHON_CMD=python
fi

#──────────────────────────────────────────────────────────────#
#                      Prerequisites Check                     #
#──────────────────────────────────────────────────────────────#
check_prerequisites() {
  log "Checking prerequisites..."

  if ! command -v jq &> /dev/null; then
    error "jq is not installed. Please install jq first."
  fi

  if ! command -v bc &> /dev/null; then
    error "bc is not installed. Please install bc first."
  fi

  if [ "$MULTI_NODE" = true ]; then
    if ! command -v docker &> /dev/null; then
      error "Docker is not installed. Please install Docker first."
    fi

    # Check for docker-compose
    if ! command -v docker-compose &> /dev/null; then
      if ! docker compose version &> /dev/null; then
        error "docker-compose is not installed. Please install docker-compose first."
      fi
    fi

    # Check for tmux
    if command -v tmux &> /dev/null; then
      TMUX_AVAILABLE=true
      log "tmux is available, will use for cluster logs."
    else
      warn "tmux is not installed. Logs will not be auto-attached in a split-screen view."
      TMUX_AVAILABLE=false
    fi
  fi

  log "Prerequisites check passed."
}

#──────────────────────────────────────────────────────────────#
#                         Cleanup Function                     #
#──────────────────────────────────────────────────────────────#
perform_cleanup() {
  log "Starting comprehensive cleanup..."

  # Kill any tmux sessions
  if command -v tmux &> /dev/null; then
    log "Stopping any tmux sessions..."
    tmux kill-session -t $TMUX_SESSION 2>/dev/null || true
  fi

  # Stop and remove Docker containers and networks for our testnet
  if command -v docker &> /dev/null; then
    log "Stopping and removing Docker containers..."
    docker-compose -f "${PROJECT_ROOT}/docker/docker-compose.multi.yml" down --remove-orphans 2>/dev/null || true

    # Remove any containers with 'sei-validator' or 'sei-coordinator' in their names
    docker ps -a | grep -E 'sei-validator|sei-coordinator' | awk '{print $1}' | xargs -r docker rm -f 2>/dev/null || true

    log "Removing Docker network 'sei-network'..."
    docker network rm sei-network 2>/dev/null || true

    # Remove Docker volumes related to sei-chain (if named accordingly)
    docker volume ls | grep sei | awk '{print $2}' | xargs -r docker volume rm 2>/dev/null || true

    # Optionally, remove the Docker image used by our testnet:
    docker rmi sei-chain/localnode 2>/dev/null || true

    # Optionally, prune the Docker system (be cautious as this removes dangling items)
    docker system prune -a --volumes -f 2>/dev/null || true
  fi

  # Remove local SEI directories
  log "Removing local Sei configuration directories..."
  rm -rf ~/.sei
  for i in {0..20}; do
    rm -rf ~/.sei-validator-$i 2>/dev/null || true
  done

  # Remove generated files from the project folder
  log "Removing generated project files..."
  rm -rf "${PROJECT_ROOT}/docker/validator-scripts" 2>/dev/null || true
  rm -f "${PROJECT_ROOT}/docker/docker-compose.multi.yml" 2>/dev/null || true
  rm -rf "${PROJECT_ROOT}/docker/shared" 2>/dev/null || true

  log "Cleanup completed successfully!"
}

#──────────────────────────────────────────────────────────────#
#                      Main Menu Function                      #
#──────────────────────────────────────────────────────────────#
show_menu() {
  clear
  echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════╗${NC}"
  echo -e "${BLUE}║                    SEI LOCAL SETUP OPTIONS                    ║${NC}"
  echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════╝${NC}"
  echo
  echo "Please select a setup option:"
  echo
  echo "  1) Default single-node testnet"
  echo "  2) Default single-node testnet (no oracle requirement)"
  echo "  3) 4-node cluster testnet (Docker)"
  echo "  4) 4-node cluster testnet (Docker, no oracle requirement)"
  echo "  5) Custom configuration (advanced options)"
  echo "  6) Resume previous deployment (skip cleanup)"
  echo "  0) Exit"
  echo
  read -p "Enter your choice [0-6]: " choice
  case $choice in
    1)
      log "Initializing default single-node testnet"
      ;;
    2)
      DISABLE_ORACLE=true
      log "Initializing default single-node testnet with oracle disabled"
      ;;
    3)
      MULTI_NODE=true
      log "Initializing 4-node cluster testnet"
      ;;
    4)
      MULTI_NODE=true
      DISABLE_ORACLE=true
      log "Initializing 4-node cluster testnet with oracle disabled"
      ;;
    5)
      CUSTOM_CONFIG=true
      custom_config
      ;;
    6)
      RESUME=true
      log "Resuming previous deployment (skipping cleanup)"
      ;;
    0)
      log "Exiting..."
      exit 0
      ;;
    *)
      error "Invalid choice. Please enter a number between 0 and 6."
      ;;
  esac
}

#──────────────────────────────────────────────────────────────#
#              Custom Configuration Function                   #
#──────────────────────────────────────────────────────────────#
custom_config() {
  echo
  echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════╗${NC}"
  echo -e "${BLUE}║                     CUSTOM CONFIGURATION                      ║${NC}"
  echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════╝${NC}"
  echo

  # Oracle requirement
  read -p "Disable oracle requirement? (y/n): " -n 1 -r
  echo
  if [[ $REPLY =~ ^[Yy]$ ]]; then
    DISABLE_ORACLE=true
    log "Oracle requirement will be disabled"
  else
    DISABLE_ORACLE=false
    log "Oracle requirement will remain enabled"
  fi

  # Single or multi-node
  read -p "Set up multiple validator nodes (cluster)? (y/n): " -n 1 -r
  echo
  if [[ $REPLY =~ ^[Yy]$ ]]; then
    MULTI_NODE=true
    read -p "Number of validator nodes (2-20) [default: 4]: " NODE_COUNT
    NODE_COUNT=${NODE_COUNT:-4}
    if ! [[ "$NODE_COUNT" =~ ^[2-9]$|^1[0-9]$|^20$ ]]; then
      NODE_COUNT=4
      warn "Invalid number. Using default: 4 nodes"
    fi
    log "Will set up a ${NODE_COUNT}-node validator cluster"
  else
    MULTI_NODE=false
    NODE_COUNT=1
    log "Will set up a single validator node"
  fi

  # Gather user-specified block parameters, consensus parameters, etc.
  read -p "Enter maximum block gas limit (1000000-1000000000) [default: $MAX_GAS]: " input_max_gas
  if [[ -n "$input_max_gas" ]]; then
    if [[ "$input_max_gas" =~ ^[0-9]+$ ]] && [ "$input_max_gas" -ge 1000000 ] && [ "$input_max_gas" -le 1000000000 ]; then
      MAX_GAS="$input_max_gas"
    else
      warn "Invalid value. Using default: $MAX_GAS"
    fi
  fi

  read -p "Enter minimum transactions per block (1-1000) [default: $MIN_TXS]: " input_min_txs
  if [[ -n "$input_min_txs" ]]; then
    if [[ "$input_min_txs" =~ ^[0-9]+$ ]] && [ "$input_min_txs" -ge 1 ] && [ "$input_min_txs" -le 1000 ]; then
      MIN_TXS="$input_min_txs"
    else
      warn "Invalid value. Using default: $MIN_TXS"
    fi
  fi

  read -p "Enter block propose timeout in ms (100-10000) [default: $PROPOSE_TIMEOUT]: " input_propose_timeout
  if [[ -n "$input_propose_timeout" ]]; then
    if [[ "$input_propose_timeout" =~ ^[0-9]+$ ]] && [ "$input_propose_timeout" -ge 100 ] && [ "$input_propose_timeout" -le 10000 ]; then
      PROPOSE_TIMEOUT="$input_propose_timeout"
    else
      warn "Invalid value. Using default: $PROPOSE_TIMEOUT"
    fi
  fi

  read -p "Enter governance voting period in seconds (10-2592000) [default: $VOTING_PERIOD]: " input_voting_period
  if [[ -n "$input_voting_period" ]]; then
    if [[ "$input_voting_period" =~ ^[0-9]+$ ]] && [ "$input_voting_period" -ge 10 ] && [ "$input_voting_period" -le 2592000 ]; then
      VOTING_PERIOD="$input_voting_period"
    else
      warn "Invalid value. Using default: $VOTING_PERIOD"
    fi
  fi

  read -p "Enter expedited voting period in seconds (10-604800) [default: $EXPEDITED_VOTING_PERIOD]: " input_expedited_voting_period
  if [[ -n "$input_expedited_voting_period" ]]; then
    if [[ "$input_expedited_voting_period" =~ ^[0-9]+$ ]] && [ "$input_expedited_voting_period" -ge 10 ] && [ "$input_expedited_voting_period" -le 604800 ]; then
      EXPEDITED_VOTING_PERIOD="$input_expedited_voting_period"
    else
      warn "Invalid value. Using default: $EXPEDITED_VOTING_PERIOD"
    fi
  fi

  # Whether to start the chain or just set up
  read -p "Set up only without starting? (y/n): " -n 1 -r
  echo
  if [[ $REPLY =~ ^[Yy]$ ]]; then
    NO_RUN=1
    log "Will set up without starting the node(s)"
  else
    NO_RUN=0
    log "Will set up and start the node(s)"
  fi

  log "Custom configuration completed. These parameters will be applied during setup."
}

#──────────────────────────────────────────────────────────────#
#                      Apply Custom Params                      #
#──────────────────────────────────────────────────────────────#
apply_custom_params() {
  log "Applying custom parameters to genesis.json..."

  # Apply consensus params
  jq --arg max_gas "$MAX_GAS" '.consensus_params["block"]["max_gas"]=$max_gas' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
  jq --arg min_txs "$MIN_TXS" '.consensus_params["block"]["min_txs_in_block"]=$min_txs' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json

  # Apply governance params
  jq --arg voting_period "${VOTING_PERIOD}s" '.app_state["gov"]["voting_params"]["voting_period"]=$voting_period' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
  jq --arg exp_voting_period "${EXPEDITED_VOTING_PERIOD}s" '.app_state["gov"]["voting_params"]["expedited_voting_period"]=$exp_voting_period' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
  jq --arg max_deposit_period "${MAX_DEPOSIT_PERIOD}s" '.app_state["gov"]["deposit_params"]["max_deposit_period"]=$max_deposit_period' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json

  # Apply oracle params
  jq --arg vote_period "$VOTE_PERIOD" '.app_state["oracle"]["params"]["vote_period"]=$vote_period' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json

  # Apply staking params
  jq --arg max_ratio "$MAX_VOTING_POWER_RATIO" '.app_state["staking"]["params"]["max_voting_power_ratio"]=$max_ratio' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json

  # Set genesis block time
  NEW_GENESIS_TIME=$(date -u -d "5 minutes" +"%Y-%m-%dT%H:%M:%SZ")
  jq --arg genesis_time "$NEW_GENESIS_TIME" '.genesis_time = $genesis_time' ~/.sei/config/genesis.json > tmp && mv tmp ~/.sei/config/genesis.json

  log "Custom parameters applied successfully!"
}

#──────────────────────────────────────────────────────────────#
#                   Configure TOML Files                       #
#──────────────────────────────────────────────────────────────#
configure_toml_files() {
  # Configure app.toml
  APP_TOML_PATH="$HOME/.sei/config/app.toml"
  sed -i.bak -e 's/# concurrency-workers = .*/concurrency-workers = 500/' $APP_TOML_PATH
  sed -i.bak -e 's/occ-enabled = .*/occ-enabled = true/' $APP_TOML_PATH
  sed -i.bak -e 's/sc-enable = .*/sc-enable = true/' $APP_TOML_PATH
  sed -i.bak -e 's/ss-enable = .*/ss-enable = true/' $APP_TOML_PATH

  # Configure config.toml
  CONFIG_PATH="$HOME/.sei/config/config.toml"
  if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    sed -i 's/mode = "full"/mode = "validator"/g' $CONFIG_PATH
    sed -i 's/indexer = \["null"\]/indexer = \["kv"\]/g' $CONFIG_PATH
    sed -i 's/timeout_prevote =.*/timeout_prevote = "2000ms"/g' $CONFIG_PATH
    sed -i 's/timeout_precommit =.*/timeout_precommit = "2000ms"/g' $CONFIG_PATH
    sed -i 's/timeout_commit =.*/timeout_commit = "2000ms"/g' $CONFIG_PATH
    sed -i 's/skip_timeout_commit =.*/skip_timeout_commit = false/g' $CONFIG_PATH
  elif [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' 's/mode = "full"/mode = "validator"/g' $CONFIG_PATH
    sed -i '' 's/indexer = \["null"\]/indexer = \["kv"\]/g' $CONFIG_PATH
    sed -i '' 's/unsafe-propose-timeout-override =.*/unsafe-propose-timeout-override = "2s"/g' $CONFIG_PATH
    sed -i '' 's/unsafe-propose-timeout-delta-override =.*/unsafe-propose-timeout-delta-override = "2s"/g' $CONFIG_PATH
    sed -i '' 's/unsafe-vote-timeout-override =.*/unsafe-vote-timeout-override = "2s"/g' $CONFIG_PATH
    sed -i '' 's/unsafe-vote-timeout-delta-override =.*/unsafe-vote-timeout-delta-override = "2s"/g' $CONFIG_PATH
    sed -i '' 's/unsafe-commit-timeout-override =.*/unsafe-commit-timeout-override = "2s"/g' $CONFIG_PATH
  else
    warn "Platform not supported. Please manually configure config.toml with the following values:"
    warn "timeout_prevote = 2000ms"
    warn "timeout_precommit = 2000ms"
    warn "timeout_commit = 2000ms"
    warn "skip_timeout_commit = false"
  fi

  log "TOML files configured."
}

#──────────────────────────────────────────────────────────────#
#               Single-Node Setup Function                     #
#──────────────────────────────────────────────────────────────#
run_single_node() {
  log "Setting up a single-node testnet..."

  # Clean up existing state
  rm -rf ~/.sei

  # Build seid
  log "Building seid..."
  cd "${PROJECT_ROOT}" && make install

  # Initialize chain and create admin key
  ~/go/bin/seid init demo --chain-id sei-chain
  ~/go/bin/seid keys add $keyname --keyring-backend test

  # Fund the admin account with various tokens
  ~/go/bin/seid add-genesis-account \
    $(~/go/bin/seid keys show $keyname -a --keyring-backend test) \
    100000000000000000000usei,100000000000000000000uusdc,100000000000000000000uatom \
    --keyring-backend test

  # Add test wallet with fixed mnemonic
  log "Adding test wallet with fixed mnemonic..."
  echo "$FIXED_MNEMONIC" | ~/go/bin/seid keys add testuser --recover --keyring-backend test
  TEST_WALLET=$(~/go/bin/seid keys show testuser -a --keyring-backend test)
  ~/go/bin/seid add-genesis-account \
    $TEST_WALLET \
    100000000000000usei --keyring-backend test

  # Create genesis transaction
  ~/go/bin/seid gentx $keyname 7000000000000usei --chain-id sei-chain --keyring-backend test

  # Set up validator pubkey
  #KEY=$(jq -c '.pub_key' ~/.sei/config/priv_validator_key.json)
  #jq --argjson key "$KEY" '.validators = [{}] | .validators[0] += {"power":"1000000","pub_key": $key}' \
  #  ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
  #mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json

  # Create additional accounts
  log "Creating additional accounts..."
  $PYTHON_CMD "${PROJECT_ROOT}/loadtest/scripts/populate_genesis_accounts.py" 20 loc
  ~/go/bin/seid collect-gentxs

  # Disable oracle requirements if specified
  if [ "$DISABLE_ORACLE" = true ]; then
    log "Disabling oracle voting requirements..."
    jq '.app_state["oracle"]["params"]["min_valid_per_window"]="0.000000000000000000"' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
    mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
    jq '.app_state["oracle"]["params"]["vote_threshold"]="0.000000000000000000"' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
    mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
    jq '.app_state["oracle"]["params"]["slash_fraction"]="0.000000000000000000"' ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
    mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
  fi

  # Apply either custom or default parameters
  if [ "$CUSTOM_CONFIG" = true ]; then
    log "Applying user custom parameters..."
    apply_custom_params
  else
    log "Applying default single-node parameters..."
    jq '.app_state["gov"]["deposit_params"]["max_deposit_period"]="60s"' \
      ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
    mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
    jq '.app_state["gov"]["voting_params"]["voting_period"]="30s"' \
      ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
    mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
    jq '.app_state["gov"]["voting_params"]["expedited_voting_period"]="10s"' \
      ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
    mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
    jq '.app_state["oracle"]["params"]["vote_period"]="2"' \
      ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
    mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
    jq '.consensus_params["block"]["max_gas"]="35000000"' \
      ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
    mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
    jq '.consensus_params["block"]["min_txs_in_block"]="0"' \
      ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
    mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
    jq '.app_state["staking"]["params"]["max_voting_power_ratio"]="0.200000000000000000"' \
      ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
    mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
  fi

  # Set up token release schedule
  START_DATE=$($PYTHON_CMD -c "from datetime import datetime; print(datetime.now().strftime('%Y-%m-%d'))")
  END_DATE_3DAYS=$($PYTHON_CMD -c "from datetime import datetime, timedelta; print((datetime.now() + timedelta(days=3)).strftime('%Y-%m-%d'))")
  END_DATE_5DAYS=$($PYTHON_CMD -c "from datetime import datetime, timedelta; print((datetime.now() + timedelta(days=5)).strftime('%Y-%m-%d'))")

  jq --arg start_date "$START_DATE" --arg end_date "$END_DATE_3DAYS" \
     '.app_state["mint"]["params"]["token_release_schedule"]=[{"start_date": $start_date, "end_date": $end_date, "token_release_amount": "999999999999"}]' \
     ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
  mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json

  jq --arg start_date "$END_DATE_3DAYS" --arg end_date "$END_DATE_5DAYS" \
     '.app_state["mint"]["params"]["token_release_schedule"] += [{"start_date": $start_date, "end_date": $end_date, "token_release_amount": "999999999999"}]' \
     ~/.sei/config/genesis.json > ~/.sei/config/tmp_genesis.json
  mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json

  # Configure app.toml and config.toml
  configure_toml_files

  # Set keyring backend
  ~/go/bin/seid config keyring-backend test

  if [ $NO_RUN -eq 1 ]; then
    log "No run flag set, exiting without starting the chain"
    exit 0
  fi

  # Start the chain
  log "Starting the single-node Sei chain..."
  log "Test wallet address: $TEST_WALLET"
  log "Test wallet mnemonic: $FIXED_MNEMONIC"

  GORACE="log_path=/tmp/race/seid_race" ~/go/bin/seid start --trace --chain-id sei-chain
}

#──────────────────────────────────────────────────────────────#
#              Create Multi-Node Docker Setup                  #
#──────────────────────────────────────────────────────────────#
#!/bin/bash
# This modification centralizes the genesis creation process.
# Instead of having validator nodes coordinate among themselves,
# the script creates all keys, gentxs, and the final genesis file
# before spinning up the Docker containers.

create_multinode_docker_setup() {
  log "Creating multi-node Docker setup with $NODE_COUNT validators..."

  # Create directories for validator scripts and shared data
  mkdir -p "${PROJECT_ROOT}/docker/validator-scripts"
  mkdir -p "${PROJECT_ROOT}/docker/shared/addresses"
  mkdir -p "${PROJECT_ROOT}/docker/shared/gentxs"
  mkdir -p "${PROJECT_ROOT}/docker/shared/genesis"
  mkdir -p "${PROJECT_ROOT}/docker/shared/node-ids"

  DOCKER_COMPOSE_FILE="${PROJECT_ROOT}/docker/docker-compose.multi.yml"

  # Create the coordinator script
  COORDINATOR_SCRIPT="${PROJECT_ROOT}/docker/coordinator.sh"
  cat > "$COORDINATOR_SCRIPT" << 'EOF'
#!/bin/bash
set -e

# The total number of validators
TOTAL_COUNT=__TOTAL_COUNT__

GENESIS_HOME=/tmp/genesis-coordinator
rm -rf "$GENESIS_HOME"
mkdir -p "$GENESIS_HOME"

echo "Coordinator: initializing chain config..."
seid init coordinator --chain-id sei-chain --home "$GENESIS_HOME"

# Wait for addresses from all validators
echo "Coordinator: waiting for addresses from all validators..."
while [ "$(ls -1 /shared/addresses | wc -l)" -lt "$TOTAL_COUNT" ]; do
  sleep 2
done
echo "Coordinator: all validator addresses found."

# Fund each validator address in genesis
for addr_file in /shared/addresses/*.addr; do
  ADDR=$(cat "$addr_file")
  seid add-genesis-account "$ADDR" 7000000000000usei --home "$GENESIS_HOME"
done
echo "Coordinator: funded each validator address in genesis."

# Publish the funded genesis so validators can generate gentxs
echo "Coordinator: publishing funded genesis so validators can gentx..."
cp "$GENESIS_HOME/config/genesis.json" /shared/genesis/genesis.json

# Wait for gentxs from validators
echo "Coordinator: waiting for $TOTAL_COUNT gentxs..."
while [ "$(ls -1 /shared/gentxs | wc -l)" -lt "$TOTAL_COUNT" ]; do
  sleep 2
done
echo "Coordinator: all gentxs received."

# Create the gentx folder and copy the gentxs from /shared/gentxs
mkdir -p "$GENESIS_HOME/config/gentx"
cp /shared/gentxs/*.json "$GENESIS_HOME/config/gentx/"

# Collect the gentxs to produce final genesis
seid collect-gentxs --home "$GENESIS_HOME"
echo "Coordinator: final genesis generated."

# Apply genesis customizations if needed
if [ "$DISABLE_ORACLE" = "true" ]; then
  echo "Coordinator: Disabling oracle voting requirements..."
  jq '.app_state["oracle"]["params"]["min_valid_per_window"]="0.000000000000000000"' "$GENESIS_HOME/config/genesis.json" > /tmp/genesis.json
  mv /tmp/genesis.json "$GENESIS_HOME/config/genesis.json"
  jq '.app_state["oracle"]["params"]["vote_threshold"]="0.000000000000000000"' "$GENESIS_HOME/config/genesis.json" > /tmp/genesis.json
  mv /tmp/genesis.json "$GENESIS_HOME/config/genesis.json"
  jq '.app_state["oracle"]["params"]["slash_fraction"]="0.000000000000000000"' "$GENESIS_HOME/config/genesis.json" > /tmp/genesis.json
  mv /tmp/genesis.json "$GENESIS_HOME/config/genesis.json"
fi

# Default params for easier local testing
jq '.app_state["gov"]["deposit_params"]["max_deposit_period"]="60s"' "$GENESIS_HOME/config/genesis.json" > /tmp/genesis.json
mv /tmp/genesis.json "$GENESIS_HOME/config/genesis.json"
jq '.app_state["gov"]["voting_params"]["voting_period"]="30s"' "$GENESIS_HOME/config/genesis.json" > /tmp/genesis.json
mv /tmp/genesis.json "$GENESIS_HOME/config/genesis.json"
jq '.app_state["gov"]["voting_params"]["expedited_voting_period"]="10s"' "$GENESIS_HOME/config/genesis.json" > /tmp/genesis.json
mv /tmp/genesis.json "$GENESIS_HOME/config/genesis.json"
jq '.app_state["oracle"]["params"]["vote_period"]="2"' "$GENESIS_HOME/config/genesis.json" > /tmp/genesis.json
mv /tmp/genesis.json "$GENESIS_HOME/config/genesis.json"
jq '.consensus_params["block"]["max_gas"]="35000000"' "$GENESIS_HOME/config/genesis.json" > /tmp/genesis.json
mv /tmp/genesis.json "$GENESIS_HOME/config/genesis.json"
jq '.consensus_params["block"]["min_txs_in_block"]="0"' "$GENESIS_HOME/config/genesis.json" > /tmp/genesis.json
mv /tmp/genesis.json "$GENESIS_HOME/config/genesis.json"
jq '.app_state["staking"]["params"]["max_voting_power_ratio"]="0.200000000000000000"' "$GENESIS_HOME/config/genesis.json" > /tmp/genesis.json
mv /tmp/genesis.json "$GENESIS_HOME/config/genesis.json"

# Publish the final genesis to a new file so validators know it's final
cp "$GENESIS_HOME/config/genesis.json" /shared/genesis/final-genesis.json
echo "Coordinator: final genesis published."

# Wait for all nodes to get their peer IDs collected
echo "Coordinator: waiting for all node IDs to be collected..."
while [ "$(ls -1 /shared/node-ids | wc -l)" -lt "$TOTAL_COUNT" ]; do
  sleep 2
done
echo "Coordinator: all node IDs collected."

# Print the discovered peer information
echo "Coordinator: Peer information discovered:"
for peer_file in /shared/node-ids/*.peer; do
  echo "- $(cat $peer_file)"
done

# Keep the container alive
echo "Coordinator: All setup complete. Keeping container alive for inspection..."
tail -f /dev/null
EOF

  chmod +x "$COORDINATOR_SCRIPT"
  sed -i "s/__TOTAL_COUNT__/${NODE_COUNT}/g" "$COORDINATOR_SCRIPT"

  # Create docker-compose file with network configuration
  cat > "$DOCKER_COMPOSE_FILE" << EOF
networks:
  sei-network:
    driver: bridge
    ipam:
      config:
        - subnet: 192.168.10.0/24

services:
  coordinator:
    container_name: sei-coordinator
    image: sei-chain/localnode
    platform: linux/x86_64
    volumes:
      - ${PROJECT_ROOT}/docker/shared:/shared:Z
      - ${COORDINATOR_SCRIPT}:/coordinator.sh:Z
    environment:
      - FIXED_MNEMONIC=${FIXED_MNEMONIC}
      - DISABLE_ORACLE=${DISABLE_ORACLE}
      - VALIDATOR_IP=${IP_ADDR}
    networks:
      sei-network:
        ipv4_address: 192.168.10.100
    command: ["/bin/bash", "/coordinator.sh"]

EOF

  # Create each validator's startup script and docker-compose service with static IPs
  for (( idx=0; idx<$NODE_COUNT; idx++ )); do
    NODE_NAME="node${idx}"
    P2P_PORT=$((26656 + (idx*10)))
    RPC_PORT=$((26657 + (idx*10)))
    API_PORT=$((1317 + (idx*10)))
    GRPC_PORT=$((9090 + (idx*10)))
    IP_ADDR="192.168.10.$((10 + idx))"

    VALIDATOR_SCRIPT="${PROJECT_ROOT}/docker/validator-scripts/validator${idx}.sh"

    # Create the validator script
    cat > "$VALIDATOR_SCRIPT" << 'EOF'
#!/bin/bash
set -e

i=__VAL_INDEX__
TOTAL_COUNT=__TOTAL_COUNT__
NODE_NAME="node${i}"
SEI_HOME=/root/.sei
P2P_PORT=26656

echo "Validator $i: clearing home and init..."
rm -rf "$SEI_HOME"/*
mkdir -p "$SEI_HOME/config"
seid init "$NODE_NAME" --chain-id sei-chain --home "$SEI_HOME"

# Create the validator key with a unique HD path
seid keys add validator \
  --hd-path "m/44'/118'/0'/0/${i}" \
  --keyring-backend test \
  --home "$SEI_HOME"

VAL_ADDR=$(seid keys show validator -a --keyring-backend test --home "$SEI_HOME")
echo "$VAL_ADDR" > /shared/addresses/validator${i}.addr

# On validator0, also create the testuser key
if [ $i -eq 0 ]; then
  echo "$FIXED_MNEMONIC" | seid keys add testuser \
    --recover \
    --keyring-backend test \
    --home "$SEI_HOME"

  # Show the testuser address for verification
  TESTUSER_ADDR=$(seid keys show testuser -a --keyring-backend test --home "$SEI_HOME")
  echo "Test wallet created with address: $TESTUSER_ADDR"
fi

# Wait for the funded genesis file from the coordinator
echo "Validator $i: waiting for funded genesis from coordinator..."
while [ ! -f /shared/genesis/genesis.json ]; do
  sleep 2
done
cp /shared/genesis/genesis.json "$SEI_HOME/config/genesis.json"
echo "Validator $i: funded genesis received."

# Generate a gentx using the updated genesis
echo "Validator $i: generating gentx..."
seid gentx validator 1000000usei \
  --chain-id sei-chain \
  --keyring-backend test \
  --home "$SEI_HOME"

# Copy gentx into /shared/gentxs
cp "$SEI_HOME/config/gentx/"*.json /shared/gentxs/validator${i}.json
echo "Validator $i: gentx placed in /shared/gentxs"

# Wait for the final genesis file from the coordinator
echo "Validator $i: waiting for final genesis from coordinator..."
while [ ! -f /shared/genesis/final-genesis.json ]; do
  sleep 2
done
cp /shared/genesis/final-genesis.json "$SEI_HOME/config/genesis.json"
echo "Validator $i: final genesis received."

# Generate and store Node ID and IP address in the required format
NODE_ID=$(seid tendermint show-node-id --home "$SEI_HOME")
IP_ADDR=${VALIDATOR_IP}
PEER_ENTRY="${NODE_ID}@${IP_ADDR}:${P2P_PORT}"
echo "$PEER_ENTRY" > /shared/node-ids/validator${i}.peer
echo "Validator $i: Node ID and IP recorded as: $PEER_ENTRY"

# ---------------------------------------------------------------
# Pre-start: update persistent peers in config.toml
# ---------------------------------------------------------------
echo "Validator $i: waiting for all ${TOTAL_COUNT} node IDs to be collected..."
while [ "$(ls -1 /shared/node-ids | wc -l)" -lt "$TOTAL_COUNT" ]; do
  sleep 2
done
echo "Validator $i: all peer data collected."

# Build the persistent peers list excluding self
PEERS=""
for peer_file in /shared/node-ids/*.peer; do
  ENTRY=$(cat "$peer_file")
  PEER_NODE_ID=$(echo "$ENTRY" | cut -d '@' -f 1)
  CURR_NODE_ID="$NODE_ID"

  if [ "$PEER_NODE_ID" != "$CURR_NODE_ID" ]; then
    if [ -n "$PEERS" ]; then
      PEERS="${PEERS},"
    fi
    PEERS="${PEERS}${ENTRY}"
  fi
done

# Update the persistent-peers field in config.toml
sed -i "s|^persistent-peers *=.*|persistent-peers = \"$PEERS\"|" "$SEI_HOME/config/config.toml"
echo "Validator $i: persistent-peers updated to: $PEERS"

# Print the persistent-peers to log for debugging
echo "Validator $i: Final persistent-peers configuration:"
grep "persistent-peers" "$SEI_HOME/config/config.toml"

# Update additional config.toml params
sed -i 's/mode = "full"/mode = "validator"/g' "$SEI_HOME/config/config.toml"
sed -i 's|^laddr = "tcp://127.0.0.1:26657"|laddr = "tcp://0.0.0.0:26657"|' "$SEI_HOME/config/config.toml"
sed -i 's|allow-duplicate-ip = false|allow-duplicate-ip = true|' "$SEI_HOME/config/config.toml"
sed -i 's|create-empty-blocks = false|create-empty-blocks = true|' "$SEI_HOME/config/config.toml"
sed -i 's/timeout_prevote =.*/timeout_prevote = "2000ms"/g' "$SEI_HOME/config/config.toml"
sed -i 's/timeout_precommit =.*/timeout_precommit = "2000ms"/g' "$SEI_HOME/config/config.toml"
sed -i 's/timeout_commit =.*/timeout_commit = "2000ms"/g' "$SEI_HOME/config/config.toml"
sed -i 's/skip_timeout_commit =.*/skip_timeout_commit = false/g' "$SEI_HOME/config/config.toml"
sed -i 's/queue-type =.*/queue-type = "simple-priority"/g' "$SEI_HOME/config/config.toml"
sed -i 's/handshake-timeout =.*/handshake-timeout = "5s"/g' "$SEI_HOME/config/config.toml"


# Update app.toml
sed -i 's/# concurrency-workers = .*/concurrency-workers = 500/' "$SEI_HOME/config/app.toml"
sed -i 's/occ-enabled = .*/occ-enabled = true/' "$SEI_HOME/config/app.toml"
sed -i 's/sc-enable = .*/sc-enable = true/' "$SEI_HOME/config/app.toml"
sed -i 's/ss-enable = .*/ss-enable = true/' "$SEI_HOME/config/app.toml"

# Now start the node
echo "Validator $i: starting seid node..."
exec seid start --home "$SEI_HOME" --chain-id sei-chain
EOF

    # Substitute placeholders with actual values
    sed -i "s/__VAL_INDEX__/${idx}/g" "$VALIDATOR_SCRIPT"
    sed -i "s/__TOTAL_COUNT__/${NODE_COUNT}/g" "$VALIDATOR_SCRIPT"
    chmod +x "$VALIDATOR_SCRIPT"

    # Variable to reference in docker-compose
    eval "VALIDATOR_SCRIPT_${idx}=$VALIDATOR_SCRIPT"

    # Add the validator service to docker-compose with static IP assignment
    cat >> "$DOCKER_COMPOSE_FILE" << EOF
  validator${idx}:
    container_name: sei-validator${idx}
    image: sei-chain/localnode
    platform: linux/x86_64
    environment:
      - FIXED_MNEMONIC=${FIXED_MNEMONIC}
      - DISABLE_ORACLE=${DISABLE_ORACLE}
      - VALIDATOR_IP=${IP_ADDR}
    volumes:
      - ${PROJECT_ROOT}/docker/shared:/shared:Z
      - ${GOCACHE}:/go/cache:Z
      - ${HOME_DIR}/.sei-validator-${idx}:/root/.sei:Z
      - ${VALIDATOR_SCRIPT}:/validator-init.sh:Z
    ports:
      - "$((26656 + idx*10)):26656"
      - "$((26657 + idx*10)):26657"
      - "$((1317 + idx*10)):1317"
      - "$((9090 + idx*10)):9090"
    networks:
      sei-network:
        ipv4_address: ${IP_ADDR}
    depends_on:
      - coordinator
    command: ["/bin/bash", "/validator-init.sh"]
EOF
  done

  # Ensure local data directories exist with proper permissions
  for (( idx=0; idx<$NODE_COUNT; idx++ )); do
    mkdir -p "${HOME_DIR}/.sei-validator-${idx}"
    chmod 777 "${HOME_DIR}/.sei-validator-${idx}"
  done

  log "Multi-node Docker configuration created at $DOCKER_COMPOSE_FILE"
}

#──────────────────────────────────────────────────────────────#
#                    Build Docker Image                        #
#──────────────────────────────────────────────────────────────#
build_docker_image() {
  log "Building Docker image for validators..."

  # Create Docker directory if it doesn't exist
  mkdir -p "${PROJECT_ROOT}/docker/localnode"

  # Create Dockerfile to build seid from source inside Docker
  DOCKERFILE="${PROJECT_ROOT}/docker/localnode/Dockerfile"
  cat > "$DOCKERFILE" << EOF
FROM golang:1.22.2-bullseye

RUN apt-get update && apt-get install -y \\
    build-essential \\
    jq \\
    git \\
    bc \\
    curl \\
    vim \\
    wget \\
    && rm -rf /var/lib/apt/lists/*

# Clone and build sei-chain inside the container
WORKDIR /sei-protocol
RUN git clone https://github.com/sei-protocol/sei-chain.git
WORKDIR /sei-protocol/sei-chain
RUN make install

# Create a script to set up the environment correctly
RUN echo '#!/bin/bash' > /usr/local/bin/setup-env.sh && \\
    echo 'export PATH=\$PATH:/root/go/bin' >> /usr/local/bin/setup-env.sh && \\
    echo 'exec "\$@"' >> /usr/local/bin/setup-env.sh && \\
    chmod +x /usr/local/bin/setup-env.sh

ENTRYPOINT ["/usr/local/bin/setup-env.sh"]
CMD ["/bin/bash"]
EOF

  # Build the docker image
  log "Building Docker image, this may take some time..."
  log "Note: This will compile seid from source inside Docker and may take several minutes."
if ! (cd "${PROJECT_ROOT}/docker/localnode" && docker build --pull -t sei-chain/localnode .); then
  error "Docker image build failed. Please check Docker installation and try again."
fi

  log "Docker image built successfully with seid compiled inside."
}

#──────────────────────────────────────────────────────────────#
#                     Run Multi-Node Cluster                   #
#──────────────────────────────────────────────────────────────#
run_multi_node() {
  log "Preparing multi-node cluster setup..."

  # Build Docker image
  build_docker_image

  # Create Docker compose and validator scripts
  create_multinode_docker_setup

  if [ $NO_RUN -eq 1 ]; then
    log "No run flag set, exiting without starting the cluster"
    exit 0
  fi

  log "Starting multi-node cluster via Docker..."
  if ! (cd "${PROJECT_ROOT}/docker" && docker-compose -f docker-compose.multi.yml up -d); then
    error "Failed to start Docker containers. Please check Docker logs for details."
  fi

  # Wait for containers to initialize
  log "Waiting for containers to initialize..."
  sleep 10

  # Check if containers are running
  if ! docker ps | grep -q sei-validator0; then
    warn "Validator containers may not have started properly. Checking status..."
    docker ps -a | grep sei-validator
    warn "You may need to check docker logs for errors. Try: docker logs sei-validator0"
  else
    log "Cluster status:"
    docker ps | grep sei-validator

    # Print test wallet info
    log "Multi-node cluster running in background."
    log "Test wallet mnemonic: $FIXED_MNEMONIC"

    # Try to get wallet address from validator0
    log "Attempting to retrieve test wallet address from validator0..."
    TEST_WALLET_ADDR=$(docker exec -it sei-validator0 seid keys show testuser -a --keyring-backend test 2>/dev/null || echo "")
    if [ -n "$TEST_WALLET_ADDR" ]; then
      log "Test wallet address: $TEST_WALLET_ADDR"
    else
      log "Could not retrieve wallet address yet. Try: docker exec -it sei-validator0 seid keys show testuser -a --keyring-backend test"
    fi
  fi

  # If tmux is available, show logs
  if [ "$TMUX_AVAILABLE" = true ]; then
    log "Setting up tmux session for node logs..."
    tmux kill-session -t $TMUX_SESSION 2>/dev/null || true

    # Create new tmux session
    tmux new-session -d -s $TMUX_SESSION "docker logs -f sei-validator0"

    # Add remaining validators
    for (( i=1; i<$NODE_COUNT; i++ )); do
      tmux split-window -v -t $TMUX_SESSION "docker logs -f sei-validator${i}"
    done

    # Arrange windows in tiled layout
    tmux select-layout -t $TMUX_SESSION tiled

    log "Attaching to tmux session for logs. Press Ctrl+B then D to detach."
    tmux attach -t $TMUX_SESSION
  else
    log "tmux not available. Use 'docker logs -f sei-validator0' to view logs."
    # Check if the container is running before trying to view logs
    if docker ps | grep -q sei-validator0; then
      docker logs -f sei-validator0
    else
      warn "Container sei-validator0 not running. Cannot display logs."
    fi
  fi
}

#──────────────────────────────────────────────────────────────#
#                       Start Oracle Voter                      #
#──────────────────────────────────────────────────────────────#
start_oracle_voter() {
  log "Starting oracle price voter..."
  PYTHON_SCRIPT="${PROJECT_ROOT}/oracle_voter.py"

  # Create oracle voter script if it doesn't exist
  cat > "$PYTHON_SCRIPT" << 'EOF'
#!/usr/bin/env python3
import time
import subprocess
import requests
import sys
import logging

# Set up logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')

# Default configuration
KEYNAME = "admin"
PASSWORD = "12345678"
CHAIN_ID = "sei-chain"
VOTE_PERIOD = 10
BINARY = "~/go/bin/seid"
NODE = "http://localhost:26657"

def get_current_vote_period():
    try:
        res = requests.get(f"{NODE}/blockchain")
        body = res.json()
        return int(body["result"]["last_height"]) // VOTE_PERIOD
    except Exception as e:
        logging.error(f"Error getting current vote period: {e}")
        return -1

def execute_cmd(cmd):
    try:
        result = subprocess.run(cmd, shell=True, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        logging.info(f"Command executed successfully: {cmd}")
        logging.debug(f"Output: {result.stdout}")
        return result.stdout
    except subprocess.CalledProcessError as e:
        logging.error(f"Command failed: {cmd}")
        logging.error(f"Error: {e.stderr}")
        return None

def get_validator_address():
    cmd = f"printf '{PASSWORD}\\n' | {BINARY} keys show {KEYNAME} --bech=val | grep address | cut -d':' -f2 | xargs"
    result = execute_cmd(cmd)
    if result:
        val_addr = result.strip()
        logging.info(f"Validator address: {val_addr}")
        return val_addr
    return None

def vote():
    val_addr = get_validator_address()
    if not val_addr:
        logging.error("Could not get validator address")
        return False

    # Construct fake prices (1 for each whitelisted token)
    prices = "1usei,1ueth,1ubtc,1uusdc,1uusdt,1uosmo,1uatom"

    # Submit vote
    cmd = f"printf '{PASSWORD}\\n' | {BINARY} tx oracle aggregate-vote {prices} {val_addr} --from {KEYNAME} --chain-id={CHAIN_ID} -y --broadcast-mode=sync"
    result = execute_cmd(cmd)
    return result is not None

def main():
    logging.info("Starting automatic oracle voter")
    logging.info(f"Chain ID: {CHAIN_ID}")
    logging.info(f"Node: {NODE}")

    last_voted_period = -1

    while True:
        try:
            current_period = get_current_vote_period()
            if current_period > last_voted_period:
                logging.info(f"Submitting oracle vote for period {current_period}")
                if vote():
                    last_voted_period = current_period
                    logging.info(f"Vote submitted successfully for period {current_period}")
                else:
                    logging.error(f"Failed to submit vote for period {current_period}")
            time.sleep(1)  # Check every second
        except Exception as e:
            logging.error(f"Unexpected error: {e}")
            time.sleep(5)  # Wait a bit longer on error

if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        logging.info("Oracle voter stopped by user")
EOF

  chmod +x "$PYTHON_SCRIPT"

  # Start the oracle voter in background
  if [ "$DISABLE_ORACLE" = false ]; then
    log "Oracle voting required. Starting oracle voter in background."
    nohup python3 "$PYTHON_SCRIPT" > oracle_voter.log 2>&1 &
    log "Oracle voter started. Logs in oracle_voter.log"
  else
    log "Oracle voting disabled. Not starting oracle voter."
  fi
}

#──────────────────────────────────────────────────────────────#
#                         Main Execution                       #
#──────────────────────────────────────────────────────────────#
# Handle command line arguments for --cleanup or --help
if [ "$1" = "--cleanup" ]; then
  perform_cleanup
  exit 0
elif [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
  echo "SEI Local Testnet Setup Script"
  echo
  echo "Usage:"
  echo "  $0                  - Run the script in interactive mode"
  echo "  $0 --cleanup        - Clean up previous deployments"
  echo "  $0 --help           - Show this help message"
  echo
  echo "This script helps set up a local Sei blockchain testnet for development and testing."
  echo "It supports single-node and multi-node configurations with customizable parameters."
  echo
  echo "Dependencies:"
  echo "  - Docker and docker-compose (for multi-node setup)"
  echo "  - jq and bc"
  echo "  - tmux (optional, for better log viewing)"
  echo
  exit 0
fi

# Check if root directory exists
if [ ! -d "$PROJECT_ROOT" ]; then
  warn "Unable to detect project root directory. Make sure you're running this script from the Sei project directory."
  PROJECT_ROOT=$(pwd)
  warn "Using current directory: $PROJECT_ROOT"
fi

# Welcome message
echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║                   SEI LOCAL TESTNET SETUP                  ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
echo
echo "This script will help you set up a local Sei blockchain testnet."
echo "You can choose between a single-node setup or a multi-node cluster."
echo "All options are customizable via the interactive menu."
echo
echo "Press Enter to continue..."
read

# Show the menu and get user configuration
show_menu

# Now, based on the menu selection, run cleanup if we're NOT resuming
if [ "$RESUME" = false ]; then
  perform_cleanup
else
  log "Skipping cleanup; resuming previous deployment."
fi

# Check prerequisites
check_prerequisites

# Run the appropriate setup based on user choice
if [ "$MULTI_NODE" = true ]; then
  run_multi_node
else
  run_single_node

  # Start oracle voter if needed
  if [ "$DISABLE_ORACLE" = false ]; then
    start_oracle_voter
  fi
fi
