import json
import time

try:
    with open("~/.lumen_wallet.txt", "r") as f:
        addr = f.read().strip()

    memo = f"x402::payout::{addr}::{int(time.time())}"
    receipt = {"wallet": addr, "memo": memo, "timestamp": time.ctime()}

    with open("receipts.json", "a") as r:
        r.write(json.dumps(receipt) + "\n")

    print("✅ x402 payout triggered (memo prepared).")

except Exception as e:
    print(f"⚠️ Error: {e}")
