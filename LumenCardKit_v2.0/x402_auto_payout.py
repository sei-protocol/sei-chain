"""x402 payout receipt generator.

This script records payout receipts for the x402 system. It reads the local
wallet (payee) from ~/.lumen_wallet.txt and prepares a memo. It supports
recording:
  - chain/network identifier (default: "sei")
  - payer address (optional)
  - amount (optional)

Each receipt is stored in receipts.json with fields:
  payer, payee, amount, memo, timestamp, chain
"""

import argparse
import json
import os
import time
from datetime import datetime, timezone


def main() -> None:
    parser = argparse.ArgumentParser(description="Prepare an x402 payout receipt")
    parser.add_argument("--chain", default="sei", help="Chain or network identifier")
    parser.add_argument("--payer", default="unknown", help="Payer address")
    parser.add_argument(
        "--amount",
        type=int,
        default=0,
        help="Payout amount (integer, e.g. token units)",
    )
    args = parser.parse_args()

    # Read payee wallet
    wallet_path = os.path.expanduser("~/.lumen_wallet.txt")
    if not os.path.exists(wallet_path):
        raise FileNotFoundError(f"Wallet file not found at {wallet_path}")
    with open(wallet_path, "r", encoding="utf-8") as f:
        payee = f.read().strip()

    # Construct memo & receipt
    now = datetime.now(timezone.utc)
    memo = f"x402::payout::{payee}::{int(now.timestamp())}"
    receipt = {
        "payer": args.payer,
        "payee": payee,
        "amount": args.amount,
        "memo": memo,
        "timestamp": now.isoformat().replace("+00:00", "Z"),
        "chain": args.chain,
    }

    # Append to receipts.json
    receipts_path = os.path.join(os.path.dirname(__file__), "receipts.json")
    if os.path.exists(receipts_path):
        try:
            with open(receipts_path, "r") as r:
                data = json.load(r)
        except json.JSONDecodeError:
            data = []
    else:
        data = []

    data.append(receipt)
    with open(receipts_path, "w") as r:
        json.dump(data, r, indent=2)

    print("✅ x402 payout receipt stored.")


if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        print(f"⚠️ Error: {e}")
