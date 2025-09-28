# Deploying `MsgClaim` transactions from Node.js

The [`deploy/msg_claim.mjs`](../deploy/msg_claim.mjs) helper sends the Sei Solo
precompile call (`claim(bytes)` or `claimSpecific(bytes)`) directly to the
network. It expects the Cosmos-signed payload that was produced off-chain by
`seid tx sign --generate-only` or another signer, then wraps and broadcasts the
EVM transaction for you.

## Prerequisites

- Node.js 18 or later
- A Sei EVM account with funds for gas
- The Cosmos-signed payload for either `MsgClaim` or `MsgClaimSpecific`
- The private key for the EVM account you will broadcast from

Install the JavaScript dependencies once per checkout:

```bash
npm install
```

## 1. Export the signing key securely

Store the private key in an environment variable for the current shell session.
Avoid pasting it into command history.

```bash
export PRIVATE_KEY="0xyour_private_key"
```

## 2. Provide the Cosmos payload

Save the signed payload to disk. The helper accepts either a binary file or a
hex string (with or without the `0x` prefix).

```bash
cat <<'PAYLOAD' > claim_payload.hex
0x0123abcd...
PAYLOAD
```

## 3. Dry-run to inspect the transaction (optional)

Use the `--dry-run` flag to preview the call data and gas configuration without
broadcasting:

```bash
node deploy/msg_claim.mjs \
  --payload claim_payload.hex \
  --claim-specific \
  --gas-limit 750000 \
  --dry-run
```

The command prints a JSON summary that includes the encoded calldata. Adjust
fees or the `--claim-specific` toggle as needed.

## 4. Broadcast the transaction

When you are ready, provide an RPC endpoint and run the script without
`--dry-run`:

```bash
node deploy/msg_claim.mjs \
  --payload claim_payload.hex \
  --rpc-url https://sei-evm-rpc.example \
  --gas-price 0.02
```

The helper automatically queries fee data from the RPC when you do not supply
`--gas-price` or the EIP-1559 flags. Set `--no-wait` if you do not want to wait
for the transaction receipt.

### Customising fees and nonce

- `--gas-price <gwei>` – Sends a legacy-style transaction.
- `--max-fee-per-gas` and `--max-priority-fee-per-gas` – Sends an EIP-1559
  transaction. You must provide both flags together.
- `--nonce <value>` – Overrides the account nonce if you need to replay or queue
  a specific sequence.

### Chain ID hints

If the RPC endpoint serves multiple networks behind the same URL, pass
`--chain-id` to enforce the expected chain ID during the connection handshake.

### Broadcasting manually

The script prints the transaction hash immediately. If you set `--no-wait` (or
terminate the script early) you can still query status later with `seid tx hash
<tx_hash>` or an RPC explorer.
