#!/bin/bash

MIN_HEIGHT=${1//\'/}  # Remove single quotes
MIN_HEIGHT=${MIN_HEIGHT//\"/}  # Remove double quotes

if [ -z "$MIN_HEIGHT" ]; then
    echo "Usage: $0 <MIN_HEIGHT>" >&2
    exit 1
fi

# Source shared helpers (_wait_until, wait_until_height_exceeds).
# Resolve path relative to this script so we work regardless of cwd.
seidbin=seid
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../../contracts/_tx_helpers.sh"

wait_until_height_exceeds "$MIN_HEIGHT" || exit 1
