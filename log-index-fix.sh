#!/bin/bash

set -euo pipefail

TX_HASH=0x6685a10343b5d3fe3bce4d90999007eeb194fe673d8597f53a89015e0b3116f4
ADDRESS=0xe737e5cebeeba77efe34d4aa090756590b1ce275
BLOCK=185811748
RPC=$1

# Get log index from cast logs
LOG_INDEX_HEX=$(cast logs --address "$ADDRESS" --from-block "$BLOCK" --to-block "$BLOCK" -r "$RPC" --json \
  | jq -r --arg addr "$ADDRESS" --arg tx "$TX_HASH" '.[] | select(.address == $addr and .transactionHash == $tx) | .logIndex' | head -n1)

# Get log index from cast receipt
RECEIPT_LOG_INDEX_HEX=$(cast receipt "$TX_HASH" -r "$RPC" --json \
  | jq -r --arg addr "$ADDRESS" '.logs[] | select(.address == $addr) | .logIndex' | head -n1)

if [[ -z "$LOG_INDEX_HEX" || -z "$RECEIPT_LOG_INDEX_HEX" ]]; then
  echo "❌ Could not find matching log index"
  echo "cast logs index: $LOG_INDEX_HEX"
  echo "receipt log index: $RECEIPT_LOG_INDEX_HEX"
  exit 1
fi

LOG_INDEX_DEC=$((16#${LOG_INDEX_HEX#0x}))
RECEIPT_LOG_INDEX_DEC=$((16#${RECEIPT_LOG_INDEX_HEX#0x}))

if [ "$LOG_INDEX_DEC" -eq "$RECEIPT_LOG_INDEX_DEC" ]; then
  echo "✅ Log indexes match: $LOG_INDEX_DEC"
else
  echo "❌ Mismatch: logs says $LOG_INDEX_DEC, receipt says $RECEIPT_LOG_INDEX_DEC"
fi
