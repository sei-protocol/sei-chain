#!/bin/bash
set -e

# === CONFIGURATION ===
keyname="x402key"
chainid="pacific-1"
seidbin="seid"

# Make sure DEPLOYED_WALLET_ADDR is set after deployment
if [ -z "$DEPLOYED_WALLET_ADDR" ]; then
  echo "[!] Please export DEPLOYED_WALLET_ADDR before running this script."
  exit 1
fi

echo "[*] Sealing x402Wallet to SoulSync Registry..."

$seidbin tx registry seal-wallet \
  --wallet=$DEPLOYED_WALLET_ADDR \
  --soul-sync \
  --royalty=11 \
  --from=$keyname \
  --chain-id=$chainid \
  --gas=250000 \
  --fees=200000usei \
  -y -b block
