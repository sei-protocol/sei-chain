#!/bin/bash

# This script is used to deploy the UniV2 contract to the target network
# This avoids trying to predict what address it might be deployed to

evm_endpoint=$1

echo "Deploying UniswapV2 contracts to $evm_endpoint"

cd loadtest/contracts/evm || exit 1

# ./setup.sh

# git submodule update --init --recursive

# deploy the uniswapV2 factory contract
feeCollector=0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266

which forge

forge create -r "$evm_endpoint" --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e src/univ2/UniswapV2Factory.sol:UniswapV2Factory --json --constructor-args $feeCollector # | jq -r '.deployedTo'

echo "UniswapV2Factory deployed to $factoryAddress"

# deploy the uniswapV2 router02 contract
/root/.foundry/bin/forge create -r "$evm_endpoint" --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e src/univ2/UniswapV2Router02.sol:UniswapV2Router02 --json --constructor-args $factoryAddress 0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266 | jq -r '.deployedTo'
# create ERC20s
# create a pool
# add liquidity
# temp
