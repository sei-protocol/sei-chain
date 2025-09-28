name: Sei USDC Payout — KinPay Flow

on:
  workflow_dispatch:
    inputs:
      recipient:
        description: "Recipient Sei address"
        required: true
      amount:
        description: "Amount in uUSDC (e.g. 1000000 = 1 USDC)"
        required: true
      memo:
        description: "Memo for attribution"
        required: false
        default: "KinPay USDC payout"
      chain_id:
        description: "Sei Chain ID (e.g. pacific-1)"
        required: true
        default: "pacific-1"
      denom:
        description: "Token denom (e.g. uusdc)"
        required: true
        default: "uusdc"

jobs:
  payout:
    runs-on: ubuntu-latest
    env:
      SEID_HOME: /home/runner/.sei
    steps:
      - name: 📥 Checkout
        uses: actions/checkout@v4

      - name: 🐍 Install Dependencies
        run: |
          sudo apt-get update && sudo apt-get install -y unzip jq
          curl -LO https://github.com/sei-protocol/sei-chain/releases/download/v4.0.1/seid_linux_amd64.zip
          unzip seid_linux_amd64.zip
          chmod +x seid
          sudo mv seid /usr/local/bin/

      - name: 🔐 Import Wallet
        run: |
          echo "${{ secrets.SEI_MNEMONIC }}" > mnemonic.txt
          echo "test1234" | seid keys add kinpay --recover --keyring-backend=file < mnemonic.txt

      - name: ⚙️ Configure Sei CLI
        run: |
          seid config chain-id ${{ github.event.inputs.chain_id }}
          seid config node https://rpc.pacific-1.sei.io
          seid config keyring-backend file
          seid config output json

      - name: 💰 Get Wallet Address
        id: wallet
        run: |
          ADDR=$(seid keys show kinpay -a --keyring-backend=file)
          echo "wallet=$ADDR" >> $GITHUB_OUTPUT

      - name: 🧾 Show Balance
        run: |
          seid query bank balances ${{ steps.wallet.outputs.wallet }}

      - name: 💸 Send Payout
        run: |
          seid tx bank send \
            ${{ steps.wallet.outputs.wallet }} \
            ${{ github.event.inputs.recipient }} \
            ${{ github.event.inputs.amount }}${{ github.event.inputs.denom }} \
            --memo "${{ github.event.inputs.memo }}" \
            --from kinpay \
            --fees 2000${{ github.event.inputs.denom }} \
            --keyring-backend file \
            --chain-id ${{ github.event.inputs.chain_id }} \
            --yes \
            --broadcast-mode block

      - name: 🧾 Show Updated Balance
        run: |
          seid query bank balances ${{ steps.wallet.outputs.wallet }}
