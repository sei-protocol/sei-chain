# 🚀 Deploying SeiBill

## Prereqs
- `forge` (Foundry)
- `seid` / Keplr wallet with testnet USDC
- Bill parser installed (Python)

## Steps

1. Compile the contract:
```bash
forge build
```

2. Deploy manually:

```bash
forge create contracts/SeiBill.sol:SeiBill --rpc-url <SEI_RPC> --constructor-args <USDC_ADDRESS>
```

3. Simulate:

```bash
forge script scripts/ScheduleAndPay.s.sol --fork-url <SEI_RPC> --broadcast
```

4. Parse a real bill:

```bash
python scripts/bill_parser.py
```
