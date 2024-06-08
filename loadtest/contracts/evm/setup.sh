#!/bin/bash

# Check if forge is installed by trying to run it and checking if the command exists
if ! command -v forge &> /dev/null
then
    echo "forge could not be found, installing Foundry..."
    curl -L https://foundry.paradigm.xyz | bash
    /root/.foundry/bin/foundryup
fi

sudo apt-get install jq -y

# Install OpenZeppelin contracts
forge install OpenZeppelin/openzeppelin-contracts --no-commit &> /dev/null