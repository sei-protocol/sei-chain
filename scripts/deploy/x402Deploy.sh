#!/bin/bash
set -e

# === CONFIGURATION ===
keyname="x402key"
chainid="pacific-1"
seidbin="seid"
rpc="https://rpc.sei-apis.com"

# === CONTRACTS (placeholders, fill once deployed) ===
jupiteraddr="REPLACE_WITH_JUPITER_CONTRACT"
marsaddr="REPLACE_WITH_MARS_CONTRACT"
saturnaddr="REPLACE_WITH_SATURN_CONTRACT"
venusaddr="REPLACE_WITH_VENUS_CONTRACT"

# === REGISTER DEX PAIRS ===
echo "[*] Registering Saturn pair..."
echo '{"batch_contract_pair":[{"contract_addr":"'"$saturnaddr"'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}]}' > saturn.json
printf "12345678\n" | $seidbin tx dex register-pairs saturn.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json

echo "[*] Registering Venus pair..."
echo '{"batch_contract_pair":[{"contract_addr":"'"$venusaddr"'","pairs":[{"price_denom":"SEI","asset_denom":"ATOM","tick_size":"0.0000001"}]}]}' > venus.json
printf "12345678\n" | $seidbin tx dex register-pairs venus.json -y --from=$keyname --chain-id=$chainid --fees=10000000usei --gas=500000 --broadcast-mode=block --output=json

# === OUTPUT ADDRESSES ===
echo "========== DEPLOYED CONTRACTS =========="
echo " Jupiter: $jupiteraddr"
echo " Mars:    $marsaddr"
echo " Saturn:  $saturnaddr"
echo " Venus:   $venusaddr"
echo "========================================"
