# Building Sei Solo claim transactions in GitHub Codespaces

The [`scripts/build_claim_tx.py`](../scripts/build_claim_tx.py) helper signs the EVM
transaction locally and writes the raw hex blob to disk so you can broadcast it
with `seid tx broadcast` or the `/txs` RPC endpoint. The workflow below walks
through using the script inside a GitHub Codespace so the private key never
leaves your isolated development environment.

> **Never hard-code or commit private keys.** Store them as Codespace secrets or
> export them only for the lifetime of your terminal session.

## 1. Launch a Codespace

1. Navigate to your fork of [`sei-chain`](https://github.com/sei-protocol/sei-chain).
2. Click **Code** → **Codespaces** → **Create codespace on main** (or the branch
   you prefer).
3. Wait for the container to build and for the web-based VS Code terminal to
   appear.

## 2. Configure Python dependencies

The default Codespace image already ships with Python 3. If you want to keep the
helper’s dependencies isolated, create a virtual environment first:

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install --upgrade pip
pip install web3 eth-account
```

If you reuse the Codespace later, just run `source .venv/bin/activate` followed
by `pip install --upgrade web3 eth-account` to pick up updates.

## 3. Provide access to the signing key

Prefer GitHub Codespaces secrets when possible:

1. Open **Repository settings** → **Codespaces secrets**.
2. Add a secret named `PRIVATE_KEY` whose value is the EVM private key.
3. Rebuild or restart the Codespace so the secret is injected.

Inside the terminal, expose the key just before running the helper:

```bash
export PRIVATE_KEY="$PRIVATE_KEY"
```

If you cannot use secrets, export the value manually in the terminal. Avoid
copying it into files that could be committed.

## 4. Load the Cosmos payload

Save the signed Cosmos transaction blob (from your off-chain signer) into the
Codespace workspace:

```bash
cat <<'PAYLOAD' > claim_payload.hex
0x0123abcd...
PAYLOAD
```

The helper accepts either a `0x`-prefixed hex string or a binary payload file.

## 5. Configure optional RPC lookups

Set the RPC endpoint if you want the helper to fetch the current nonce and gas
price automatically:

```bash
export SEI_EVM_RPC_URL="https://evm-rpc.sei.example"
```

Skip this step and pass `--nonce` and fee flags explicitly if you prefer full
manual control.

## 6. Build the transaction

Run the helper from the repository root:

```bash
python scripts/build_claim_tx.py \
  --payload claim_payload.hex \
  --gas-limit 750000 \
  --chain-id 1329 \
  --output signed_claim.json
```

Use `--claim-specific` when your payload wraps `MsgClaimSpecific`. To pin legacy
fees and the nonce explicitly:

```bash
python scripts/build_claim_tx.py \
  --payload claim_payload.hex \
  --gas-limit 750000 \
  --chain-id 1329 \
  --gas-price 0.02 \
  --nonce 7
```

Both commands print the raw transaction hex blob to stdout and write a JSON
summary to `signed_claim.json`.

## 7. Retrieve the signed transaction

Right-click `signed_claim.json` in the VS Code Explorer and choose **Download**
if you need the file locally. Alternatively, copy the `raw_transaction` field
from the terminal output.

## 8. Broadcast manually

Broadcast the raw hex from a trusted environment using either pattern:

```bash
seid tx broadcast signed_claim.json
# or
curl -X POST "https://sei-rpc.example/txs" \
  -H 'Content-Type: application/json' \
  -d '{"tx_bytes": "<raw_hex>", "mode": "BROADCAST_MODE_SYNC"}'
```

Replace `sei-rpc.example` and the broadcast mode according to your
infrastructure needs.

## Troubleshooting tips

- `ValueError: Payload hex must have an even number of characters` – double
  check the payload file; each byte needs two hexadecimal characters.
- `Nonce is required when no RPC endpoint is available` – pass `--nonce` or set
  `SEI_EVM_RPC_URL`.
- `ModuleNotFoundError: No module named 'eth_account'` – activate your virtual
  environment and reinstall dependencies with `pip install web3 eth-account`.
