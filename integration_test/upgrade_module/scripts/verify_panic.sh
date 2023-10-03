#!/bin/bash

# Verify the service is NOT running
if pgrep -f "seid start --chain-id sei" > /dev/null; then
   echo "FAIL"
   exit 1
fi

echo "PASS"
exit 0
