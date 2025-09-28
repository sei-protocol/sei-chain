# 🚀 Deploying SeiBill

## Prereqs
- `forge` (Foundry)
- `seid` / Keplr wallet with testnet USDC
- Bill parser installed (Python)

## Steps

1. Compile the contract and regenerate the ABI:
```bash
forge build
forge inspect contracts/SeiBill.sol:SeiBill abi > abi/SeiBill.json
```

2. Deploy manually:

```bash
forge create contracts/SeiBill.sol:SeiBill --rpc-url <SEI_RPC> --constructor-args <USDC_ADDRESS>
```

3. Configure a bridge router:

```bash
cast send <CONTRACT_ADDRESS> "setBridgeRouter(address,bool)" <ROUTER_ADDRESS> true --rpc-url <SEI_RPC> --private-key <DEPLOYER_KEY>
```

4. Simulate:

```bash
forge script scripts/ScheduleAndPay.s.sol --fork-url <SEI_RPC> --broadcast
```

5. Parse a real bill:

```bash
python scripts/bill_parser.py
```
