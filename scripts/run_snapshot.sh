#!/bin/bash

# Check if the correct number of arguments is provided
if [ "$#" -ne 3 ]; then
    echo "Usage: $0 INITIAL_HALT_HEIGHT SNAPSHOT_INTERVAL CHAIN_ID"
    exit 1
fi

INITIAL_HALT_HEIGHT=$1
SNAPSHOT_INTERVAL=$2
CHAIN_ID=$3

# Define variables for paths based on the home directory
SEI_DIR="$HOME/.sei"
CONFIG_FILE="$SEI_DIR/config/app.toml"
SNAPSHOT_DIR="$HOME/snapshots"
SEID_BIN="$HOME/go/bin/seid"

# Ensure the seid binary exists
if [ ! -x "$SEID_BIN" ]; then
    echo "Error: seid binary not found at $SEID_BIN"
    exit 1
fi

# Stop the seid service if it's managed by systemd
if systemctl is-active --quiet seid; then
    systemctl stop seid
    echo "Stopped seid service."
else
    echo "seid service is not running."
fi

# Update pruning settings in the configuration file
if [ -f "$CONFIG_FILE" ]; then
    sed -i -e 's/pruning = .*/pruning = "custom"/' "$CONFIG_FILE"
    sed -i -e 's/pruning-keep-recent = .*/pruning-keep-recent = "1"/' "$CONFIG_FILE"
    sed -i -e 's/pruning-keep-every = .*/pruning-keep-every = "0"/' "$CONFIG_FILE"
    sed -i -e 's/pruning-interval = .*/pruning-interval = "1"/' "$CONFIG_FILE"
    echo "Updated pruning settings in $CONFIG_FILE."
else
    echo "Error: Configuration file $CONFIG_FILE not found."
    exit 1
fi

# Create the snapshots directory if it doesn't exist
mkdir -p "$SNAPSHOT_DIR"
echo "Ensured snapshot directory exists at $SNAPSHOT_DIR."

# Initialize halt height
HALT_HEIGHT=$INITIAL_HALT_HEIGHT

# Start the snapshot loop
while true
do
    # Update the halt height in the configuration file
    sed -i -e "s/halt-height = .*/halt-height = $HALT_HEIGHT/" "$CONFIG_FILE"
    echo "Set halt-height to $HALT_HEIGHT in $CONFIG_FILE."

    # Start the seid node with tracing
    echo "Starting seid node with chain ID $CHAIN_ID and halt height $HALT_HEIGHT."
    "$SEID_BIN" start --trace --chain-id "$CHAIN_ID" &
    SEID_PID=$!

    # Wait for the node to initialize (you might need to adjust the sleep duration)
    sleep 10

    # Take a snapshot at the current halt height
    start_time=$(date +%s)
    "$SEID_BIN" tendermint snapshot "$HALT_HEIGHT"
    end_time=$(date +%s)
    elapsed=$(( end_time - start_time ))
    echo "Backed up snapshot for height $HALT_HEIGHT which took $elapsed seconds."

    # Increment the halt height for the next snapshot
    HALT_HEIGHT=$(( HALT_HEIGHT + SNAPSHOT_INTERVAL ))
    echo "Next halt height set to $HALT_HEIGHT."

    # Navigate back to the home directory
    cd "$HOME"
done
