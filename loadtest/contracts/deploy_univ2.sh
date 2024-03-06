#!/bin/bash

# This script is used to deploy the UniV2 contract to the target network
# This avoids trying to predict what address it might be deployed to

evm_endpoint=$1

echo "Deploying UniswapV2 contracts to $evm_endpoint"

cd loadtest/contracts/evm || exit 1

# ./setup.sh

# git submodule update --init --recursive

# deploy the uniswapV2 factory contract
feeCollector=0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266 # first anvil address, just need a random address
wallet=0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52

factoryAddress=$(forge create -r "$evm_endpoint" --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e src/univ2/UniswapV2Factory.sol:UniswapV2Factory --json --constructor-args $feeCollector | jq -r '.deployedTo')

echo "UniswapV2Factory deployed to $factoryAddress"

# deploy the uniswapV2 router02 contract
routerAddress=$(forge create -r "$evm_endpoint" --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e src/univ2/UniswapV2Router.sol:UniswapV2Router --json --constructor-args $factoryAddress $feeCollector | jq -r '.deployedTo')

echo "UniswapV2Router deployed to $routerAddress"

# create ERC20s
token1Address=$(forge create -r "$evm_endpoint" --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e src/ERC20Token.sol:ERC20Token --json --constructor-args "Token1" "T1" | jq -r '.deployedTo')

echo "Token1 deployed to $token1Address"

token2Address=$(forge create -r "$evm_endpoint" --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e src/ERC20Token.sol:ERC20Token --json --constructor-args "Token2" "T2" | jq -r '.deployedTo')

echo "Token2 deployed to $token2Address"

# mint tokens
cast send -r "$evm_endpoint" $token1Address --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e "mint(address,uint256)" $wallet 1000000000 --legacy

cast send -r "$evm_endpoint" $token2Address --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e "mint(address,uint256)" $wallet 1000000000 --legacy

balanceToken1=$(cast call -r "$evm_endpoint" $token1Address "balanceOf(address)" $wallet)
balanceToken2=$(cast call -r "$evm_endpoint" $token2Address "balanceOf(address)" $wallet)

echo "Token1 balance: $balanceToken1"
echo "Token2 balance: $balanceToken2"

# create a pool
echo "Creating a pool"
cast send -r "$evm_endpoint" $factoryAddress --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e "createPair(address,address)" $token1Address $token2Address --legacy --json

# get the pair address
pairAddress=$(cast call -r "$evm_endpoint" $factoryAddress "getAllPairsIndex(uint256)" 0)

echo "Pair address: $pairAddress"

echo "Approving router to spend tokens..."
cast send -r "$evm_endpoint" $token1Address --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e "approve(address,uint256)" $routerAddress 1000000000 --legacy --json
cast send -r "$evm_endpoint" $token2Address --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e "approve(address,uint256)" $routerAddress 1000000000 --legacy --json

echo "Check allowance..."
token1ToRouterAllowance=$(cast call -r "$evm_endpoint" $token1Address "allowance(address,address)(uint256)" $wallet $routerAddress)
token2ToRouterAllowance=$(cast call -r "$evm_endpoint" $token2Address "allowance(address,address)(uint256)" $wallet $routerAddress)

echo "Token1 to router allowance: $token1ToRouterAllowance"
echo "Token2 to router allowance: $token2ToRouterAllowance"

echo "Adding liquidity to pool"
echo "cast send -r $evm_endpoint $routerAddress --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e 'addLiquidity(address,address,uint256,uint256,uint256,uint256,address,uint256)' \
    $token1Address $token2Address 1000000 1000000 0 0 $wallet 1000000000000000000 --legacy --json"
cast send -r "$evm_endpoint" $routerAddress --private-key 57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e "addLiquidity(address,address,uint256,uint256,uint256,uint256,address,uint256)" \
    $token1Address $token2Address 1000000 1000000 0 0 $wallet 1000000000000000000 --legacy --json
