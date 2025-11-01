
#!/usr/bin/env bash
set -euo pipefail

# x402.sh — royalty owed table generator
# Usage: ./x402.sh ./x402/receipts.json > owed.txt

INPUT_FILE="${1:-}"

if [[ -z "$INPUT_FILE" ]]; then
  echo "❌ Usage: $0 <receipts.json>" >&2
  exit 1
fi

if [[ ! -f "$INPUT_FILE" ]]; then
  echo "❌ File not found: $INPUT_FILE" >&2
  exit 1
fi

echo "🔎 Processing receipts from $INPUT_FILE"
echo "----------------------------------------"

TOTAL=0

# Example: each receipt JSON contains { "amount": 100, "payer": "...", "payee": "..." }
jq -r '.[] | [.payer, .payee, .amount] | @tsv' "$INPUT_FILE" | while IFS=$'\t' read -r PAYER PAYEE AMOUNT; do
  echo "PAYER: $PAYER → PAYEE: $PAYEE | AMOUNT: $AMOUNT"
  TOTAL=$((TOTAL + AMOUNT))
done

echo "----------------------------------------"
echo "TOTAL OWED: $TOTAL"
