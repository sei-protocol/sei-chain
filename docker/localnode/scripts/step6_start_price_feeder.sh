#!/usr/bin/env sh

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
printf "12345678\n" | "$HOME/go/bin/seid" tx oracle set-feeder "$ORACLE_ACCOUNT_ADDRESS" --from $VALIDATOR_ACCOUNT --fees 2000usei -b block -y --chain-id sei >/dev/null 2>&1
printf "12345678\n" | "$HOME/go/bin/seid" tx bank send $VALIDATOR_ACCOUNT "$ORACLE_ACCOUNT_ADDRESS" --from $VALIDATOR_ACCOUNT 1000sei --fees 2000usei -b block -y >/dev/null 2>&1


sed -i.bak -e "s|^address *=.*|address = \"$ORACLE_ACCOUNT_ADDRESS\"|" $ORACLE_CONFIG_FILE
sed -i.bak -e "s|^validator *=.*|validator = \"$SEIVALOPER\"|" $ORACLE_CONFIG_FILE


# Starting oracle price feeder
echo "Starting the oracle price feeder daemon"
printf "12345678\n" | price-feeder "$ORACLE_CONFIG_FILE" > "$LOG_DIR/price-feeder-$NODE_ID.log" 2>&1 &
echo "Node $NODE_ID started successfully! Check your logs under $LOG_DIR/"
