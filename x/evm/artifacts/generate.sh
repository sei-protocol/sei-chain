#!/bin/bash

# Function to check if a command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Check if solc is installed
if ! command_exists solc; then
    echo "Error: solc is not installed."
    echo "Please install solc using one of the following methods:"
    echo "  - For Ubuntu: sudo add-apt-repository ppa:ethereum/ethereum && sudo apt-get update && sudo apt-get install solc"
    echo "  - For macOS: brew install solidity"
    echo "  - For other systems: See https://soliditylang.org/install/"
    exit 1
fi

# Check if abigen is installed
if ! command_exists abigen; then
    echo "Error: abigen is not installed."
    echo "Please install abigen using the following command:"
    echo "  - go install github.com/ethereum/go-ethereum/cmd/abigen@latest"
    echo "Make sure your GOPATH is set and that the bin directory is in your PATH."
    exit 1
fi

# Check solc version
SOLC_VERSION="0.8.25"
INSTALLED_SOLC_VERSION=$(solc --version | grep -oP 'Version: \K[0-9.]+')
if [[ $INSTALLED_SOLC_VERSION != $SOLC_VERSION ]]; then
    echo "Error: solc version $SOLC_VERSION is required. Currently installed version is $INSTALLED_SOLC_VERSION."
    echo "Please install the correct version."
    exit 1
fi

# Define the function to compile and clean up a contract
compile_and_cleanup() {
    local dir=$1
    local sol_file=$2
    local base_name="${sol_file%.sol}"

    # Set the output directory
    OUTPUT_DIR="x/evm/artifacts/$dir"

    # Create the output directory if it doesn't exist
    mkdir -p $OUTPUT_DIR

    # Build the contract with optimization enabled
    solc --optimize --overwrite @openzeppelin=contracts/lib/openzeppelin-contracts --bin -o $OUTPUT_DIR contracts/src/$sol_file
    solc --optimize --overwrite @openzeppelin=contracts/lib/openzeppelin-contracts --abi -o $OUTPUT_DIR contracts/src/$sol_file

    # Remove extra ABI and BIN files, but exclude the main contract's bin and abi, legacy.bin, and all .go files
    find $OUTPUT_DIR -type f \( -name '*.abi' -o -name '*.bin' \) ! -name "$base_name.*" ! -name 'legacy.bin' ! -name '*.go' -delete

    # Generate Go bindings
    abigen --abi=$OUTPUT_DIR/$base_name.abi --pkg=$dir --out=$OUTPUT_DIR/$dir.go
}

# Invoke the function for each contract
compile_and_cleanup "cw20" "CW20ERC20Pointer.sol"
compile_and_cleanup "cw721" "CW721ERC721Pointer.sol"
compile_and_cleanup "native" "NativeSeiTokensERC20.sol"
