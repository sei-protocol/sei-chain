# LumenCardKit

Utilities for issuing and managing LumenCard wallets. The scripts in this
folder are intentionally simple so they can be adapted to your own workflow or
chain integration.

## Included scripts

- `sunset_wallet.py` – generate a new wallet and sigil file.
- `generate_qr_code.py` – create a QR code from the sigil.
- `lumen_checkout.py` – output an ephemeral checkout session identifier.
- `fund_lumen_wallet.sh` – simulate funding the generated wallet.
- `send_lumen_email.py` – email the wallet and sigil using a local SMTP server.
- `x402_auto_payout.py` – produce x402 compatible payout receipts.

## x402 integration

`x402_auto_payout.py` reads the payee wallet from `~/.lumen_wallet.txt` and
optionally accepts a payer address and amount:

```bash
python x402_auto_payout.py <payer> <amount>
```

Each execution appends a JSON object to `receipts.json` containing the payer,
payee, amount, memo and timestamp.  These receipts can then be aggregated into a
royalty table using `x402.sh`:

```bash
./x402.sh receipts.json
```

This workflow mirrors the examples in the [x402 documentation](https://www.docs.sei.io/)
and allows LumenCard payments to be tracked alongside other x402 transactions.

## License

This directory is licensed under the [MIT License](LICENSE). You're free to use,
modify, and distribute these utilities subject to the terms of that license.
