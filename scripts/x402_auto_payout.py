#!/usr/bin/env python3
import os
import json
import requests

# Required env vars
PRIVATE_KEY = os.getenv("X402_PRIVATE_KEY")
RPC_URL = os.getenv("SEI_RPC_URL")
WALLET = os.getenv("X402_WALLET_ADDRESS")

if not all([PRIVATE_KEY, RPC_URL, WALLET]):
    print("❌ Missing secrets.")
    exit(1)

# Load owed.txt
with open("owed.txt") as f:
    lines = f.readlines()

# Extract total owed from last line
owed_line = [line for line in lines if line.startswith("TOTAL OWED")]
if not owed_line:
    print("⚠️ No TOTAL OWED line found.")
    exit(0)

total_owed = owed_line[0].split(":")[1].strip()
if total_owed == "0":
    print("💤 Nothing owed. Skipping payout.")
    exit(0)

print(f"💸 Total owed: {total_owed}")

# Simulated send — replace this with real wallet signing logic
print(f"🔐 Sending payment from {WALLET} to recipients...")
print("✅ Payment sent successfully (simulated).")
