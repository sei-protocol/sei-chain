# Benchmark Scenarios

This folder contains benchmark scenario configurations for `benchmark/benchmark.sh`.

## Usage

```bash
# Use default scenario (EVMTransfer)
./benchmark/benchmark.sh

# Use ERC20 scenario
BENCHMARK_CONFIG=benchmark/scenarios/erc20.json ./benchmark/benchmark.sh

# Use mixed scenario (EVMTransfer + ERC20)
BENCHMARK_CONFIG=benchmark/scenarios/mixed.json ./benchmark/benchmark.sh
```

## Available Scenarios

### `default.json`
Simple EVM native token transfers. No contract deployment required.
- **Scenarios**: EVMTransfer (weight: 1)
- **Accounts**: 5000

### `erc20.json`
ERC20 token transfers. Requires contract deployment during setup phase.
- **Scenarios**: ERC20 (weight: 1)
- **Accounts**: 5000

### `mixed.json`
Combination of native transfers and ERC20 transfers.
- **Scenarios**: EVMTransfer (weight: 3), ERC20 (weight: 1)
- **Accounts**: 5000

## Configuration Format

Configurations follow the sei-load `LoadConfig` format:

```json
{
  "accounts": {
    "count": 5000,           // Number of accounts to generate
    "newAccountRate": 0.0    // Rate of new account creation (0.0 = fixed pool)
  },
  "scenarios": [
    {
      "name": "EVMTransfer",  // Scenario name (see list below)
      "weight": 1             // Relative weight for weighted random selection
    }
  ]
}
```

## Supported Scenario Names

| Name | Description | Requires Deployment |
|------|-------------|---------------------|
| `EVMTransfer` | Native token transfers | No |
| `EVMTransferNoop` | No-op transfers | No |
| `ERC20` | ERC20 token transfers | Yes |
| `ERC721` | ERC721 NFT transfers | Yes |
| `ERC20Conflict` | ERC20 with conflict patterns | Yes |
| `ERC20Noop` | ERC20 no-op transfers | Yes |
| `Disperse` | Batch token dispersal | Yes |

## Two-Phase Execution

Scenarios that require deployment go through a **setup phase** before load generation:

1. **Setup Phase**: Deployment transactions are created and processed. After each block,
   receipts are checked for deployed contract addresses.

2. **Load Phase**: Once all contracts are deployed, the benchmark generates load
   transactions according to the configured scenario weights.

You'll see log messages indicating phase transitions:
```
benchmark generator config txsPerBatch=1000
Scenario doesn't need deployment, attaching with zero address scenario=evmtransfer
Created deployment transaction scenario=erc20 txHash=0x...
Contract deployed scenario=erc20 address=0x...
All scenarios deployed, transitioning to load phase
Load generator initialized scenarios=2
```
