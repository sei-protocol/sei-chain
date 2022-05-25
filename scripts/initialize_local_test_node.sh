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
go build -o build/seid ./cmd/seid/
echo $password | sudo -S rm -r ~/.sei-chain/
echo $password | sudo -S rm -r ~/test_accounts/
./build/seid unsafe-reset-all
./build/seid init demo --chain-id sei-chain
yes | ./build/seid keys add $keyname
yes | ./build/seid keys add faucet
./build/seid add-genesis-account $(./build/seid keys show $keyname -a) 100000000000000000000ust
./build/seid add-genesis-account $(./build/seid keys show faucet -a) 100000000000000000000ust
python ./loadtest/scripts/populate_genesis_accounts.py $numtestaccount loc
./build/seid gentx $keyname 70000000000000000000ust --chain-id sei-chain
./build/seid collect-gentxs
cat ~/.sei-chain/config/genesis.json | jq '.app_state["crisis"]["constant_fee"]["denom"]="ust"' > ~/.sei-chain/config/tmp_genesis.json && mv ~/.sei-chain/config/tmp_genesis.json ~/.sei-chain/config/genesis.json
cat ~/.sei-chain/config/genesis.json | jq '.app_state["gov"]["deposit_params"]["min_deposit"][0]["denom"]="ust"' > ~/.sei-chain/config/tmp_genesis.json && mv ~/.sei-chain/config/tmp_genesis.json ~/.sei-chain/config/genesis.json
cat ~/.sei-chain/config/genesis.json | jq '.app_state["mint"]["params"]["mint_denom"]="ust"' > ~/.sei-chain/config/tmp_genesis.json && mv ~/.sei-chain/config/tmp_genesis.json ~/.sei-chain/config/genesis.json
cat ~/.sei-chain/config/genesis.json | jq '.app_state["staking"]["params"]["bond_denom"]="ust"' > ~/.sei-chain/config/tmp_genesis.json && mv ~/.sei-chain/config/tmp_genesis.json ~/.sei-chain/config/genesis.json
./build/seid start --trace
