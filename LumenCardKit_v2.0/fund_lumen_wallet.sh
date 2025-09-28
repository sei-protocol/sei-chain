#!/bin/bash
echo "💸 Simulating manual wallet funding..."

ADDR=$(cat ~/.lumen_wallet.txt)
echo "Funding wallet address: $ADDR"
echo "Done. (Simulated only — integrate with your chain to enable live fund)"
