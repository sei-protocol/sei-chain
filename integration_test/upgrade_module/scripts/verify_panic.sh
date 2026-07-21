#!/bin/bash

if [ -n "$1" ]; then
    echo "Usage: $0"
    exit 1
fi

max_attempts=60
attempt=0

# Try for 1 minute to see if the service is NOT running
while [ $attempt -lt $max_attempts ]; do
    if pgrep -f "seid start --chain-id sei" > /dev/null; then
        sleep 1  # wait for 1 second before checking again
        attempt=$((attempt+1))
    else
        echo "PASS"
        exit 0
    fi
done

echo "FAIL"
exit 1
