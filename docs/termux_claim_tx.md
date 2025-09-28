# Building Sei Solo claim transactions in Termux

The [`scripts/build_claim_tx.py`](../scripts/build_claim_tx.py) helper signs the EVM
transaction locally and writes the raw hex blob to disk so you can broadcast it
with `seid tx broadcast` or the `/txs` RPC endpoint. The steps below assume you
already have:

- a Sei EVM account with enough ETH to cover gas,
- the Cosmos-signed payload produced off-chain for either `MsgClaim` or
  `MsgClaimSpecific`, and
- the corresponding private key you want to use for the EVM transaction.

> **Never hard-code or commit private keys.** Always load them at runtime from
> an environment variable or prompt.

## 1. Prepare Termux

```bash
pkg update
pkg install -y python git clang make openssl
python -m ensurepip --upgrade
pip install --upgrade pip virtualenv
```

## 2. Grab the helper and its dependencies

```bash
cd ~
git clone https://github.com/sei-protocol/sei-chain.git
cd sei-chain
python -m venv .venv
source .venv/bin/activate
pip install web3 eth-account
```

If you already have a checkout, just pull the latest changes and upgrade the
Python dependencies inside your virtual environment.

## 3. Export the signing key

Termux shells reset environment variables between sessions, so export the key
immediately before running the script:

```bash
export PRIVATE_KEY="0xyour_private_key_without_quotes"
```

For better hygiene, consider using `read -s` to prompt for the value each time
instead of storing it in shell history.

## 4. Provide the Cosmos payload

Save the signed Cosmos transaction blob (from your off-chain signer) to a file.
For example:

```bash
cat <<'PAYLOAD' > claim_payload.hex
0x0123abcd...
PAYLOAD
```

The helper accepts either a `0x`-prefixed hex string or a binary file.

## 5. Query account metadata (optional)

If you know the nonce and fees you want to use, you can skip this step. When
connected to an RPC endpoint, the script will fetch both automatically:

```bash
export SEI_EVM_RPC_URL="https://evm-rpc.sei.example"
```

## 6. Build the transaction

```bash
python scripts/build_claim_tx.py \
  --payload claim_payload.hex \
  --gas-limit 750000 \
  --chain-id 1329 \
  --output signed_claim.json
```

Use `--claim-specific` when your payload wraps `MsgClaimSpecific`. To override
fees explicitly:

```bash
python scripts/build_claim_tx.py \
  --payload claim_payload.hex \
  --gas-limit 750000 \
  --chain-id 1329 \
  --gas-price 0.02 \
  --nonce 7
```

Both invocations print the raw transaction hex blob to stdout and save a JSON
summary in `signed_claim.json`.

## 7. Broadcast manually

Copy the raw hex (`raw_transaction`) into a file, then broadcast from another
machine or via Termux using either of the following patterns:

```bash
seid tx broadcast signed_claim.json
# or
curl -X POST "https://sei-rpc.example/txs" \
  -H 'Content-Type: application/json' \
  -d '{"tx_bytes": "<raw_hex>", "mode": "BROADCAST_MODE_SYNC"}'
```

Replace `sei-rpc.example` and the broadcast mode as required by your
infrastructure.

## Troubleshooting tips

- `ValueError: Payload hex must have an even number of characters` – double
  check the payload file; each byte needs two hexadecimal characters.
- `Nonce is required when no RPC endpoint is available` – pass `--nonce` or set
  `SEI_EVM_RPC_URL`.
- `Unable to connect to RPC` – verify the endpoint URL and that Termux has
  network connectivity.
