#!/bin/bash

PROGRESS_ACCOUNT=${1//\'/}
PROGRESS_ACCOUNT=${PROGRESS_ACCOUNT//\"/}

if [ -z "$PROGRESS_ACCOUNT" ]; then
    echo "Usage: $0 <PROGRESS_ACCOUNT>"
    exit 1
fi

max_attempts=60
attempt=0
PROGRESS_ADDR=$(printf "12345678\n" | seid keys show "$PROGRESS_ACCOUNT" -a 2>/dev/null)
if [ -z "$PROGRESS_ADDR" ]; then
    echo "Unable to resolve progress account $PROGRESS_ACCOUNT"
    exit 1
fi

# Try for 1 minute to see if the service is NOT running
# As long as it is running, we preemptively generate traffic
# to force the chain to generate blocks, in case the panic height has not been reached yet.
while [ $attempt -lt $max_attempts ]; do
    if pgrep -f "seid start --chain-id sei" > /dev/null; then
        printf "12345678\n" | seid tx bank send "$PROGRESS_ACCOUNT" "$PROGRESS_ADDR" 1usei \
            -y --chain-id sei --fees 2000usei --broadcast-mode sync >/dev/null 2>&1 || true
        sleep 0.5
        attempt=$((attempt+1))
    else
        echo "PASS"
        exit 0
    fi
done

echo "FAIL"
exit 1
