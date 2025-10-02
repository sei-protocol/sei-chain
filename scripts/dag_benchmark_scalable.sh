#!/bin/bash

set -e

CHAIN_ID="sei-local"
NODE="http://localhost:26657"
FROM="tester"
DENOM="usei"
DEST="sei1destinationaddresshere"
AUTHORITY=$(seid keys show "$FROM" -a)
TX_COUNT=100     # default txs
LOG_FILE="dag_benchmark_log.csv"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

# Handle optional args
while [[ "$#" -gt 0 ]]; do
  case $1 in
    --tx-count) TX_COUNT="$2"; shift ;;
    --dest) DEST="$2"; shift ;;
    --log) LOG_FILE="$2"; shift ;;
    *) echo "Unknown param: $1"; exit 1 ;;
  esac
  shift
done

# Init CSV if missing
if [ ! -f "$LOG_FILE" ]; then
  echo "timestamp,tx_count,block_height,duration_ms" > "$LOG_FILE"
fi

echo "🔐 Setting access mapping for bank/MsgSend..."

seid tx accesscontrol set-access "$AUTHORITY" bank/MsgSend '[
  {"type":1,"resource_id":"bank/%s"},
  {"type":2,"resource_id":"bank/%s"}
]' \
  --from "$FROM" --chain-id "$CHAIN_ID" --yes --broadcast-mode block --node "$NODE"

echo "💰 Creating and funding $TX_COUNT stress accounts..."

for i in $(seq 1 $TX_COUNT); do
  KEY="stress$i"
  if ! seid keys show "$KEY" &> /dev/null; then
    seid keys add "$KEY" --keyring-backend test > /dev/null
  fi
  ADDR=$(seid keys show "$KEY" -a --keyring-backend test)
  seid tx bank send "$FROM" "$ADDR" 100$DENOM \
    --from "$FROM" --chain-id "$CHAIN_ID" --yes --broadcast-mode block --node "$NODE"
done

echo "🧪 Preparing $TX_COUNT MsgSend txs..."

DIR="stresstest_batch_$TX_COUNT"
mkdir -p "$DIR" && cd "$DIR"
rm -f *.json

for i in $(seq 1 $TX_COUNT); do
  KEY="stress$i"
  ADDR=$(seid keys show "$KEY" -a --keyring-backend test)
  seid tx bank send "$ADDR" "$DEST" 1$DENOM \
    --generate-only --chain-id "$CHAIN_ID" > tx$i.json
done

echo "✍️ Signing txs..."

for i in $(seq 1 $TX_COUNT); do
  seid tx sign tx$i.json \
    --from "stress$i" --keyring-backend test \
    --chain-id "$CHAIN_ID" --output-document tx$i.signed.json
done

echo "🕒 Starting timed broadcast..."

START_HEIGHT=$(curl -s "$NODE/status" | jq -r '.result.sync_info.latest_block_height')
START_TIME=$(date +%s%3N)

seid tx broadcast-batch $(ls *.signed.json) \
  --node "$NODE" --chain-id "$CHAIN_ID" --yes --broadcast-mode block

echo "⏳ Awaiting next block..."

while :; do
  HEIGHT=$(curl -s "$NODE/status" | jq -r '.result.sync_info.latest_block_height')
  if [ "$HEIGHT" -gt "$START_HEIGHT" ]; then
    break
  fi
  sleep 0.2
done

END_TIME=$(date +%s%3N)
DURATION=$((END_TIME - START_TIME))

echo "✅ Block $HEIGHT confirmed"
echo "📊 Elapsed: ${DURATION}ms for $TX_COUNT txs"

cd ..

# Log to CSV
echo "$TIMESTAMP,$TX_COUNT,$HEIGHT,$DURATION" >> "$LOG_FILE"
echo "📁 Logged to $LOG_FILE"
