#!/bin/bash

seidbin=$(which ~/go/bin/seid | tr -d '"')
keyname=$(printf "12345678\n" | $seidbin keys list --output json | jq ".[0].name" | tr -d '"')
chainid=$($seidbin status | jq ".NodeInfo.network" | tr -d '"')
seihome=$(git rev-parse --show-toplevel | tr -d '"')
migration=$1

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
printf "12345678\n" | $seidbin tx bank send admin "$key_admin1" 10000000sei -y --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block
printf "12345678\n" | $seidbin tx bank send admin "$key_admin2" 10000000sei -y --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block
printf "12345678\n" | $seidbin tx bank send admin "$key_admin3" 10000000sei -y --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block
printf "12345678\n" | $seidbin tx bank send admin "$key_admin4" 10000000sei -y --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block
printf "12345678\n" | $seidbin tx bank send admin "$key_op" 10000000sei -y --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block
printf "12345678\n" | $seidbin tx bank send admin "$key_staking" 10000000sei -y --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block
printf "12345678\n" | $seidbin tx bank send admin "$key_unlock" 10000000sei -y --chain-id=$chainid --gas=5000000 --fees=1000000usei --broadcast-mode=block


# Deploy goblin contract
contract_name=goblin
cd $seihome || exit
echo "Deploying $contract_name contract..."

# store
echo "Storing contract..."
store_result=$(printf "12345678\n" | $seidbin tx wasm store integration_test/contracts/"$contract_name".wasm -y --from="$keyname" --chain-id="$chainid" --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
contract_id=$(echo "$store_result" | jq -r '.logs[].events[].attributes[] | select(.key == "code_id").value')
echo "Got $contract_name contract id: $contract_id"

# instantiate
echo "Instantiating contract..."
params='{"admins":["'$key_admin1'", "'$key_admin2'", "'$key_admin3'", "'$key_admin4'"], "max_voting_period": {"time":1800}, "admin_voting_threshold_percentage": 75}'
instantiate_result=$(printf "12345678\n" | $seidbin tx wasm instantiate "$contract_id" "$params" -y --no-admin --amount=1500000usei --from="$keyname" --chain-id="$chainid" --gas=5000000 --fees=1000000usei --broadcast-mode=block --label=$contract_name --output=json)
contract_addr=$(echo "$instantiate_result" |jq -r '.logs[].events[].attributes[] | select(.key == "_contract_address").value')
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

# store
echo "Storing contract..."
store_result=$(printf "12345678\n" | $seidbin tx wasm store integration_test/contracts/"$contract_name".wasm -y --from="$keyname" --chain-id="$chainid" --gas=5000000 --fees=1000000usei --broadcast-mode=block --output=json)
contract_id=$(echo "$store_result" | jq -r '.logs[].events[].attributes[] | select(.key == "code_id").value')
echo "Got $contract_name contract id: $contract_id"

# instantiate
echo "Instantiating contract..."
VESTING_TIMESTAMPS='["1893456000000000000", "1924992000000000000"]' # nanoseconds since unix epoch
VESTING_AMOUNTS='["1000000", "500000"]' # in usei
params='{"admins":["'$key_admin1'", "'$key_admin2'", "'$key_admin3'", "'$key_admin4'"], "ops": ["'$key_op'"], "tranche": {"denom":"usei", "vesting_timestamps":'$VESTING_TIMESTAMPS', "vesting_amounts":'$VESTING_AMOUNTS', "unlocked_token_distribution_address": "'$key_unlock'", "staking_reward_distribution_address": "'$key_staking'"}, "max_voting_period": {"time":1800}, "admin_voting_threshold_percentage": 75}'
instantiate_result=$(printf "12345678\n" | $seidbin tx wasm instantiate "$contract_id" "$params" -y --admin="$goblin_addr" --amount=1500000usei --from="$keyname" --chain-id="$chainid" --gas=5000000 --fees=1000000usei --broadcast-mode=block --label=$contract_name --output=json)
contract_addr=$(echo "$instantiate_result" |jq -r '.logs[].events[].attributes[] | select(.key == "_contract_address").value')
echo "Instantiated $contract_name contract address: $contract_addr"
echo "$contract_addr,$contract_id" > $seihome/integration_test/contracts/"$contract_name"-contract-addr.txt
if [ -z "$contract_addr" ]
then
  echo "Failed to deploy contract $contract_name"
  exit 1
fi

exit 0
