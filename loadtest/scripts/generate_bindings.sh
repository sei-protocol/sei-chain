#!/bin/bash

cd bindings || exit 1

# Loop through the subdirectories
for dir in */ ; do
    # Navigate into the token's directory
    cd "$dir"
    # Check if abi.json exists in the directory
    if [ -f "abi.json" ]; then
        # Use abigen to generate the Go binding
        # The package name and output file are based on the directory name (token type)
        packageName=$(basename "$dir")
        abigen --abi="abi.json" --pkg="$packageName" --out="${packageName}.go"
        echo "Generated binding for $packageName"
    else
        echo "abi.json not found in $dir"
    fi
    # Navigate back to the evm directory
    cd .. || exit 1
done
