#!/bin/bash

# SEIDBIN / FIXTURE_SIGNER let a non-docker caller repoint the binary + signer
# without changing docker's behavior (both unset → the original computed values).
seidbin=${SEIDBIN:-$(which ~/go/bin/seid | tr -d '"')}
keyname=${FIXTURE_SIGNER:-$(printf "12345678\n" | $seidbin keys list --output json | jq ".[0].name" | tr -d '"')}
chainid=$($seidbin status | jq ".NodeInfo.network" | tr -d '"')
seihome=$(git rev-parse --show-toplevel | tr -d '"')
migration=$1

source "$(dirname "$0")/../utils/_tx_helpers.sh"

# Prepare admin accounts
echo "Preparing admin accounts..."
printf "12345678\n" | $seidbin keys add admin1
printf "12345678\n" | $seidbin keys add admin2
printf "12345678\n" | $seidbin keys add admin3
printf "12345678\n" | $seidbin keys add admin4
printf "12345678\n" | $seidbin keys add op
printf "12345678\n" | $seidbin keys add staking_reward_dest
printf "12345678\n" | $seidbin keys add unlocked_dest
key_admin1=$(printf "12345678\n" |$seidbin keys show admin1 -a)
key_admin2=$(printf "12345678\n" |$seidbin keys show admin2 -a)
key_admin3=$(printf "12345678\n" |$seidbin keys show admin3 -a)
key_admin4=$(printf "12345678\n" |$seidbin keys show admin4 -a)
key_op=$(printf "12345678\n" |$seidbin keys show op -a)
key_staking=$(printf "12345678\n" |$seidbin keys show staking_reward_dest -a)
key_unlock=$(printf "12345678\n" |$seidbin keys show unlocked_dest -a)
# bank_send_and_wait blocks until each tx commits (sender sequence
# advance) so consecutive sends from `admin` don't race on a stale
# sequence read.
bank_send_and_wait admin "$key_admin1" 10000000sei || exit 1
bank_send_and_wait admin "$key_admin2" 10000000sei || exit 1
bank_send_and_wait admin "$key_admin3" 10000000sei || exit 1
bank_send_and_wait admin "$key_admin4" 10000000sei || exit 1
bank_send_and_wait admin "$key_op" 10000000sei || exit 1
bank_send_and_wait admin "$key_staking" 10000000sei || exit 1
bank_send_and_wait admin "$key_unlock" 10000000sei || exit 1


# Deploy goblin contract
contract_name=goblin
cd $seihome || exit
echo "Deploying $contract_name contract..."

echo "Storing contract..."
contract_id=$(store_wasm integration_test/contracts/"$contract_name".wasm) || exit 1
echo "Got $contract_name contract id: $contract_id"

echo "Instantiating contract..."
params='{"admins":["'$key_admin1'", "'$key_admin2'", "'$key_admin3'", "'$key_admin4'"], "max_voting_period": {"time":1800}, "admin_voting_threshold_percentage": 75}'
contract_addr=$(instantiate_wasm "$contract_id" "$params" "$contract_name" --no-admin --amount=1500000usei) || exit 1
echo "Instantiated $contract_name contract address: $contract_addr"
echo "$contract_addr,$contract_id" > $seihome/integration_test/contracts/"$contract_name"-contract-addr.txt
if [ -z "$contract_addr" ]
then
  echo "Failed to deploy contract $contract_name"
  exit 1
fi

# Deploy gringotts contract
goblin_addr=$contract_addr
if [ -z "$migration" ]
then
  contract_name=gringotts
else
  contract_name=gringotts_migrate
fi
cd $seihome || exit
echo "Deploying $contract_name contract..."

echo "Storing contract..."
contract_id=$(store_wasm integration_test/contracts/"$contract_name".wasm) || exit 1
echo "Got $contract_name contract id: $contract_id"

echo "Instantiating contract..."
VESTING_TIMESTAMPS='["1893456000000000000", "1924992000000000000"]' # nanoseconds since unix epoch
VESTING_AMOUNTS='["1000000", "500000"]' # in usei
params='{"admins":["'$key_admin1'", "'$key_admin2'", "'$key_admin3'", "'$key_admin4'"], "ops": ["'$key_op'"], "tranche": {"denom":"usei", "vesting_timestamps":'$VESTING_TIMESTAMPS', "vesting_amounts":'$VESTING_AMOUNTS', "unlocked_token_distribution_address": "'$key_unlock'", "staking_reward_distribution_address": "'$key_staking'"}, "max_voting_period": {"time":1800}, "admin_voting_threshold_percentage": 75}'
contract_addr=$(instantiate_wasm "$contract_id" "$params" "$contract_name" --admin="$goblin_addr" --amount=1500000usei) || exit 1
echo "Instantiated $contract_name contract address: $contract_addr"
echo "$contract_addr,$contract_id" > $seihome/integration_test/contracts/"$contract_name"-contract-addr.txt
if [ -z "$contract_addr" ]
then
  echo "Failed to deploy contract $contract_name"
  exit 1
fi

exit 0
