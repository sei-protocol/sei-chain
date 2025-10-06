#!/usr/bin/env bash
set -euo pipefail

# Usage: ./x402.sh ./x402/receipts.json

RECEIPTS_FILE="${1:-./x402/receipts.json}"

if [ ! -f "$RECEIPTS_FILE" ]; then
  echo "ERROR: receipts.json not found at $RECEIPTS_FILE" >&2
  exit 1
fi

echo "🔒 x402 Settlement Table"
echo "------------------------------"
echo "Author         | Amount Owed"
echo "------------------------------"

total=0

# Loop through the JSON array and print each line
jq -r '.[] | "\(.author) \(.amount)"' "$RECEIPTS_FILE" | while read -r author amount; do
  printf "%-14s | %s\n" "$author" "$amount"
  total=$(echo "$total + $amount" | bc)
done

echo "------------------------------"
echo "TOTAL OWED: $total"
