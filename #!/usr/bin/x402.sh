#!/usr/bin/env bash
set -e

file="$1"

if [ ! -f "$file" ]; then
  echo "No receipts.json found"
  echo "TOTAL OWED: 0"
  exit 0
fi

echo "TXID        AMOUNT   CCY   PRIVATE"
echo "------------------------------------"

total=0
while read -r txid amount currency status to memo; do
  if [ "$status" = "owed" ]; then
    echo "$txid   $amount   $currency   $memo"
    total=$(echo "$total + $amount" | bc)
  fi
done < <(jq -r '.[] | "\(.txid) \(.amount) \(.currency) \(.status) \(.to) \(.memo_private)"' "$file")

echo ""
echo "TOTAL OWED: $total USDC"
