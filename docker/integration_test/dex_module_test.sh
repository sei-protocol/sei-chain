#!/bin/bash

# Prepare contracts
mkdir -p ./loadtest/contracts/mars/artifacts
aws s3 cp --region us-east-2 s3://sei-artifacts-us-east-2/contracts/mars/mars.wasm ./loadtest/contracts/mars/artifacts/mars.wasm

# Deploy contracts
sei_home=$(git rev-parse --show-toplevel | tr -d '"')
docker exec -i sei-node0 loadtest/contracts/deploy_mars_contract.sh
contract_addr=$(head -1 loadtest/contracts/contract_output.txt)

# Place orders
echo "Place a new order"
printf "12345678\n" | docker exec -i sei-node0 build/seid tx dex place-orders "$contract_addr" 'LONG?1.01?5?SEI?ATOM?LIMIT?{"leverage":"1","position_effect":"Open"}' --amount=1000000000usei -y --from=admin --chain-id=sei --fees=1000000usei --gas=50000000 --broadcast-mode=block
sleep 15
echo "Verify order is placed successfully"
result=$(docker exec -i sei-node0 build/seid q dex get-orders-by-id "$contract_addr" SEI ATOM 0 |grep "status:" |awk '{print $2}')
if [ "$result" = "PLACED" ]
then
  echo "Successfully placed an order"
else
  echo "Failed to place an order"
  exit 1
fi