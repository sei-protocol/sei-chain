#!/bin/bash

echo -n OS Password:
read -s password
echo
echo -n Key Name:
read keyname
echo
echo -n Number of Test Accounts:
read numtestaccount
echo

docker stop jaeger
docker rm jaeger
docker run -d --name jaeger \
  -e COLLECTOR_ZIPKIN_HOST_PORT=:9411 \
  -p 5775:5775/udp \
  -p 6831:6831/udp \
  -p 6832:6832/udp \
  -p 5778:5778 \
  -p 16686:16686 \
  -p 14250:14250 \
  -p 14268:14268 \
  -p 14269:14269 \
  -p 9411:9411 \
  jaegertracing/all-in-one:1.33

echo "Building..."
make install
echo $password | sudo -S rm -r ~/.sei/
echo $password | sudo -S rm -r ~/test_accounts/
~/go/bin/seid tendermint unsafe-reset-all
~/go/bin/seid init demo --chain-id sei-chain
yes | ~/go/bin/seid keys add $keyname
yes | ~/go/bin/seid keys add faucet
printf '12345678\n' | ~/go/bin/seid add-genesis-account $(~/go/bin/seid keys show $keyname -a) 100000000000000000000usei
printf '12345678\n' | ~/go/bin/seid add-genesis-account $(~/go/bin/seid keys show faucet -a) 100000000000000000000usei
python ./loadtest/scripts/populate_genesis_accounts.py $numtestaccount loc
printf '12345678\n' | ~/go/bin/seid gentx $keyname 70000000000000000000usei --chain-id sei-chain
~/go/bin/seid collect-gentxs
cat ~/.sei/config/genesis.json | jq '.app_state["crisis"]["constant_fee"]["denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["deposit_params"]["min_deposit"][0]["denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["mint"]["params"]["mint_denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["staking"]["params"]["bond_denom"]="usei"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["deposit_params"]["max_deposit_period"]="300s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.app_state["gov"]["voting_params"]["voting_period"]="5s"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
cat ~/.sei/config/genesis.json | jq '.consensus_params["block"]["time_iota_ms"]="50"' > ~/.sei/config/tmp_genesis.json && mv ~/.sei/config/tmp_genesis.json ~/.sei/config/genesis.json
~/go/bin/seid start --trace