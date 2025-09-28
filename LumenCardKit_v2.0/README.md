# LumenCardKit v2.0

Utilities for preparing automated payouts from Lumen cards.

## Cross-chain payouts

USDC payouts can occur on different chains. Move the required USDC to the
destination chain via Circle's Cross-Chain Transfer Protocol (CCTP) before
generating a receipt. Once the funds arrive, run the payout helper and
specify the chain so downstream tools can bucket receipts per chain.

```
python x402_auto_payout.py --chain sei
```

If `--chain` is omitted the script defaults to `sei`.

To inspect receipts for a specific chain you can use `jq`:

```
jq '.[] | select(.chain=="sei")' receipts.json
```

## `receipts.json` schema

Payout receipts are stored in `receipts.json` as a JSON array. Each entry
contains:

```
{
  "wallet": "<recipient wallet address>",
  "memo": "x402::payout::<wallet>::<timestamp>",
  "timestamp": "<human readable time>",
  "chain": "<chain identifier>"
}
```

Tools such as `x402.sh` can group payouts by the `chain` field to generate
reports or initiate on-chain payouts.