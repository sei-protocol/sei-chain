#!/bin/bash

# Check if forge is installed by trying to run it and checking if the command exists
if ! command -v forge &> /dev/null
then
    echo "forge could not be found, installing Foundry..."
    # Install foundryup
    curl -L https://foundry.paradigm.xyz | bash
    # Source the user's profile to make foundryup available in the current session
    # Assuming bash is the shell, adjust accordingly if using a different shell
    source ~/.bashrc || source ~/.bash_profile || source ~/.profile
    foundryup
fi

# Install OpenZeppelin contracts
forge install OpenZeppelin/openzeppelin-contracts --no-commit &> /dev/null