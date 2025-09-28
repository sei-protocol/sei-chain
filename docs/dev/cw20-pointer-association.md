# CW20 → Contract Send: Required Association

On Sei, when sending CW20 tokens to another contract (not user wallet), the receiver must be explicitly associated to an EVM address.

## Why?
Because pointer-based routing uses EVM↔CW lookups internally. If the contract is not associated, the message cannot resolve properly, causing generic wasm errors.

## Required Command
```bash
seid tx evm associate-contract-address <receiver_contract_address> \
  --from <your_wallet> \
  --fees 20000usei \
  --chain-id pacific-1 \
  -b block
```

## Example Send Command (After Association)

```bash
seid tx wasm execute <cw20_sender> \
  '{
    "send": {
      "contract": "<receiver>",
      "amount": "10",
      "msg": "eyJzdGFrZSI6IHt9fQ=="
    }
  }' \
  --from <your_wallet> \
  --fees 500000usei \
  --gas 200000 \
  --chain-id pacific-1 \
  -b block
```

## Why it matters for Sei Giga

This step prevents:

* Silent tx failures
* Pointer event loss
* Gas waste & retries
* Bottlenecks under throughput stress

Include this in your contract deployment process.
