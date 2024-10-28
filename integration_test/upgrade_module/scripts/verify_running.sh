#!/bin/bash

max_attempts=60
attempt=0

# Try for 1 minute to see if the service is running
while [ $attempt -lt $max_attempts ]; do
    if pgrep -f "seid start --chain-id sei" > /dev/null; then
        echo "PASS"
        exit 0
    fi
    sleep 1  # wait for 1 second before checking again
    attempt=$((attempt+1))
done

echo "FAIL"
exit 1
