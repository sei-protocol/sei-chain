#!/bin/bash

echo -n Sei executable:
read sei
echo
echo -n DEX contract {.wasm file}:
read contract
echo
echo -n Key Name:
read keyname
echo
echo -n Price denominator {e.g. sei}:
read pdenom
echo
echo -n Asset denominator {e.g. atom}:
read adenom
echo

$sei tx wasm store $contract -y --from=$keyname --chain-id=sei-chain --gas=3000000 --fees=100000usei --broadcast-mode=block

addr=$($sei tx wasm instantiate 1 '{}' -y --no-admin --from=$keyname --chain-id=sei-chain --gas=1500000 --fees=100000usei --broadcast-mode=block --label=dex | grep -A 1 "key: _contract_address" | sed -n 's/.*value: //p' | xargs)

$sei tx dex register-contract $addr 1 -y --from=$keyname --chain-id=sei-chain --fees=10000000usei --gas=500000 --broadcast-mode=block
$sei tx dex register-pair $addr $pdenom $adenom -y --from=$keyname --chain-id=sei-chain --fees=10000000usei --gas=500000 --broadcast-mode=block

$sei tx dex place-orders $addr Long,1.01,5,$pdenom,$adenom,Open,Limit,1 --amount=10000000usei -y --from=ta0 --chain-id=sei-chain --fees=1000000usei --gas=50000000 --broadcast-mode=block
$sei tx dex place-orders $addr Short,0.99,5,$pdenom,$adenom,Open,Limit,2 --amount=10000000usei -y --from=ta1 --chain-id=sei-chain --fees=1000000usei --gas=50000000 --broadcast-mode=block
$sei tx dex place-orders $addr Long,0.99,5,$pdenom,$adenom,Open,Limit,1 --amount=10000000usei -y --from=ta0 --chain-id=sei-chain --fees=1000000usei --gas=50000000 --broadcast-mode=block
$sei tx dex place-orders $addr Short,1.01,5,$pdenom,$adenom,Open,Limit,2 --amount=10000000usei -y --from=ta1 --chain-id=sei-chain --fees=1000000usei --gas=50000000 --broadcast-mode=block
$sei tx dex place-orders $addr Long,0.98,3,$pdenom,$adenom,Open,Limit,1 --amount=10000000usei -y --from=ta1 --chain-id=sei-chain --fees=1000000usei --gas=50000000 --broadcast-mode=block
$sei tx dex place-orders $addr Short,1.02,3,$pdenom,$adenom,Open,Limit,2 --amount=10000000usei -y --from=ta0 --chain-id=sei-chain --fees=1000000usei --gas=50000000 --broadcast-mode=block
