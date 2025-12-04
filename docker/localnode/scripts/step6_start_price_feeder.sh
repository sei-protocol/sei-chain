#!/usr/bin/env sh

set -eu

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required but not found in PATH" >&2
  exit 1
fi

run_tx() {
  desc="$1"
  shift
  echo "$desc"
  if ! TX_OUTPUT=$(printf "12345678\n" | "$HOME/go/bin/seid" "$@" --output json); then
    echo "$TX_OUTPUT"
    echo "Command failed for: seid $*" >&2
    exit 1
  fi
  echo "$TX_OUTPUT"
  code=$(echo "$TX_OUTPUT" | jq -r '.code // 0')
  if [ "$code" != "0" ]; then
    echo "Transaction returned non-zero ABCI code $code" >&2
    exit 1
  fi
}

NODE_ID=${ID:-0}

LOG_DIR="build/generated/logs"
mkdir -p $LOG_DIR
ORACLE_CONFIG_FILE="build/generated/node_$NODE_ID/price_feeder_config.toml"
ORACLE_ACCOUNT="oracle"
VALIDATOR_ACCOUNT="node_admin"

# Create an oracle account
printf "12345678\n" | "$HOME/go/bin/seid" keys add $ORACLE_ACCOUNT --output json > "$HOME/.sei/config/oracle_key.json"
ORACLE_ACCOUNT_ADDRESS=$(printf "12345678\n" | "$HOME/go/bin/seid" keys show $ORACLE_ACCOUNT -a)
SEIVALOPER=$(printf "12345678\n" | "$HOME/go/bin/seid" keys show $VALIDATOR_ACCOUNT --bech=val -a)

echo "Ensuring validator $SEIVALOPER exists on-chain before setting feeder..."
for i in $(seq 1 60); do
  if printf "12345678\n" | "$HOME/go/bin/seid" q staking validator "$SEIVALOPER" >/dev/null 2>&1; then
    break
  fi
  echo "Validator $SEIVALOPER not found yet, retrying ($i/60)..."
  sleep 2
  if [ "$i" -eq 60 ]; then
    echo "Validator $SEIVALOPER still not found after waiting" >&2
    exit 1
  fi
done

run_tx "Delegating oracle feeder for validator $SEIVALOPER to $ORACLE_ACCOUNT_ADDRESS" \
  tx oracle set-feeder "$ORACLE_ACCOUNT_ADDRESS" --from $VALIDATOR_ACCOUNT --fees 2000usei -b block -y --chain-id sei
run_tx "Funding oracle account $ORACLE_ACCOUNT_ADDRESS from $VALIDATOR_ACCOUNT" \
  tx bank send $VALIDATOR_ACCOUNT "$ORACLE_ACCOUNT_ADDRESS" --from $VALIDATOR_ACCOUNT 1000sei --fees 2000usei -b block -y


sed -i.bak -e "s|^address *=.*|address = \"$ORACLE_ACCOUNT_ADDRESS\"|" $ORACLE_CONFIG_FILE
sed -i.bak -e "s|^validator *=.*|validator = \"$SEIVALOPER\"|" $ORACLE_CONFIG_FILE


# Starting oracle price feeder
echo "Starting the oracle price feeder daemon"
printf "12345678\n" | price-feeder "$ORACLE_CONFIG_FILE" > "$LOG_DIR/price-feeder-$NODE_ID.log" 2>&1 &
echo "Node $NODE_ID started successfully! Check your logs under $LOG_DIR/"
