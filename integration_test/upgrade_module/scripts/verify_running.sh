#!/bin/bash

# Verify the service is running
if pgrep -f "seid start --chain-id sei" > /dev/null; then
   echo "PASS"
   exit 0
fi
echo "FAIL"
exit 1