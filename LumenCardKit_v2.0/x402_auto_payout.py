"""Create a payout receipt for LumenCardKit using x402 semantics.

This script reads the local wallet address and prepares a memo that can be
used for an x402 payment.  It optionally accepts the payer address and amount
as command line arguments so the resulting receipt works with the `x402.sh`
royalty table generator.

Usage:
    python x402_auto_payout.py <payer> <amount>

If no payer or amount is provided the fields will default to "unknown" and 0
respectively.
"""

import json
import os
import sys
import time


def main() -> None:
    try:
        with open(os.path.expanduser("~/.lumen_wallet.txt"), "r") as f:
            payee = f.read().strip()

        payer = sys.argv[1] if len(sys.argv) > 1 else "unknown"
        amount = int(sys.argv[2]) if len(sys.argv) > 2 else 0

        memo = f"x402::payout::{payee}::{int(time.time())}"
        receipt = {
            "payer": payer,
            "payee": payee,
            "amount": amount,
            "memo": memo,
            "timestamp": time.ctime(),
        }

        receipts: list[dict]
        try:
            with open("receipts.json", "r") as r:
                receipts = json.load(r)
        except FileNotFoundError:
            receipts = []

        receipts.append(receipt)

        with open("receipts.json", "w") as r:
            json.dump(receipts, r, indent=2)

        print("✅ x402 payout triggered (receipt stored).")

    except Exception as e:  # pragma: no cover - script style error handling
        print(f"⚠️ Error: {e}")


if __name__ == "__main__":
    main()
