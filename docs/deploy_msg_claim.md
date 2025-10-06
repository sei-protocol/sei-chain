# Deploying Sei Solo claim transactions

The [`deploy/msg_claim.ts`](../deploy/msg_claim.ts) helper signs and optionally
broadcasts the Solo precompile call that wraps a Cosmos-signed `MsgClaim` or
`MsgClaimSpecific` payload. Use it when you want a single command to sign with
an EVM key, push the transaction to a Sei EVM RPC endpoint, and optionally wait
for confirmation.

## Prerequisites

Before running the script, make sure you have:

- Node.js 18 or later.
- A Sei EVM account funded with enough ETH to cover gas.
- The signed Cosmos payload produced by `seid tx evm print-claim` (or
  `print-claim-specific`).
- The EVM private key that should own the resulting transaction.

Install the JavaScript dependencies once per checkout:

```bash
npm install
```

> The root `package.json` wires up [`tsx`](https://github.com/esbuild-kit/tsx)
> so that TypeScript helpers like `deploy/msg_claim.ts` can run without a
> separate build step.

## Step-by-step

1. **Export the signing key** (never hard-code the value):

   ```bash
   export PRIVATE_KEY="0xyour_private_key"
   ```

   For improved hygiene, use `read -s PRIVATE_KEY` so the value is not stored in
   your shell history.

2. **Save the Cosmos payload** to a file or copy it as a hex string. Example:

   ```bash
   seid tx evm print-claim 0xClaimerAddress \
     --from your-cosmos-key \
     --chain-id atlantic-2 \
     --gas-prices 0.025usei \
     --gas auto \
     --gas-adjustment 1.5 \
     --generate-only > claim_payload.hex
   ```

3. **Run the deployment helper**. Provide either the payload file or the raw hex
   string, along with your preferred fee settings:

   ```bash
   npm run deploy:msg-claim -- \
     --payload claim_payload.hex \
     --rpc-url https://sei-evm-rpc.example \
     --chain-id 1329 \
     --gas-limit 750000 \
     --gas-price 0.02
   ```

   Use `--claim-specific` when the payload wraps `MsgClaimSpecific`. To switch to
   EIP-1559 fees, pass both `--max-fee-per-gas` and `--max-priority-fee-per-gas`.
   If you omit fee flags, the script asks the RPC endpoint for suggestions.

4. **Review the output**. The helper prints the raw signed transaction, followed
   by the broadcast hash. Pass `--output signed_claim.json` to save a summary for
   future reference.

5. **Optional flags**:

   - `--no-broadcast` (or `--dry-run`) signs without sending.
   - `--no-wait` skips waiting for the transaction receipt.
   - `--nonce` lets you override the account nonce when necessary.
   - `--rpc-url` falls back to `SEI_EVM_RPC_URL` when omitted.

When broadcasting is disabled, copy the `raw_transaction` hex into `seid tx
broadcast` or `/txs` RPC calls just like the Python helper.
