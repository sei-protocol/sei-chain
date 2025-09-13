# SeiBill – USDC Bill Autopay on Sei

**SeiBill** turns x402 + USDC into a full autopay system, backed by AI.  
Users authorize once, then AI parses bills and triggers USDC payments on their behalf — rent, utilities, credit cards, etc.

## 🔁 Flow

1. Upload or email a bill.
2. AI parses payee, amount, due date.
3. Contract schedules or triggers USDC transfer.
4. Optional: Mint receipt NFT for proof.

## 🧠 Components

- `SeiBill.sol`: Contract to manage payment authorization, execution, and optional receipts.
- `bill_parser.py`: OCR + LLM AI agent that reads bill PDFs and produces payment metadata.
- `cctp_bridge.py`: Helper script that uses Circle's Cross-Chain Transfer Protocol (CCTP) to move USDC across chains.
- `x402`: Used for sovereign key-based auth and payment proof.
- `USDC`: Main settlement unit.

## 🔗 CCTP Bridge

Circle's CCTP API lets SeiBill burn USDC on one chain and mint it on another.

### Setup

1. Generate a Circle API key and export it.

   ```bash
   export CIRCLE_API_KEY=your_key_here
   ```

2. Determine the numeric IDs for the source and destination chains. Common examples:

   | Chain        | ID |
   | ------------ | -- |
   | Ethereum     | 1  |
   | Avalanche    | 2  |
   | Sei Testnet  | 3  |

### Example flow

```bash
python scripts/cctp_bridge.py \
  --from-chain 1 \
  --to-chain 3 \
  --tx-hash 0xabc123 \
  --amount 10 \
  --api-key $CIRCLE_API_KEY
```

The script burns 10 USDC on chain `1`, mints it on chain `3`, and prints an x402-style receipt confirming the transfer.

## 🚀 Deployment

See [deploy.md](deploy.md)

## License

Apache-2.0
