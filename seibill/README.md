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
- `x402`: Used for sovereign key-based auth and payment proof.
- `USDC`: Main settlement unit.

## 🚀 Deployment

See [deploy.md](deploy.md)

## License

MIT
