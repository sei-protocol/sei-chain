#!/usr/bin/env bash

# This script is used to run the LittDB benchmark.

# Find the directory of this script
SCRIPT_DIR=$(dirname "$(readlink -f "$0")")

# Get the absolute path to the binary.
BINARY_PATH="$SCRIPT_DIR/../bin/benchmark"
BINARY_PATH="$(cd "$(dirname "$BINARY_PATH")" && pwd)/$(basename "$BINARY_PATH")"

CONFIG_PATH=""${1}
if [ -z "$CONFIG_PATH" ]; then
    echo "Usage: $0 <config_path>"
    exit 1
fi
CONFIG_PATH="$(cd "$(dirname "$CONFIG_PATH")" && pwd)/$(basename "$CONFIG_PATH")"

$BINARY_PATH $CONFIG_PATH
