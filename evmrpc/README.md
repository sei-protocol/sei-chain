# Sei's EVM RPC

Sei provides a comprehensive RPC interface that combines standard Ethereum JSON-RPC compatibility with Sei-specific enhancements. This documentation covers both the standard [Ethereum JSON-RPC API](https://ethereum.org/en/developers/docs/apis/json-rpc/) endpoints and Sei's custom extensions.

## Understanding Sei's RPC Architecture

### Eth_ Endpoints
The `eth_` prefixed endpoints provide a pure EVM-compatible view of the Sei chain. These endpoints:
- Only process and return EVM transactions
- Ignore Cosmos-native transactions
- Maintain full compatibility with Ethereum tooling and libraries
- Are ideal for EVM-only applications and tools

### Sei_ Endpoints
The `sei_` prefixed endpoints provide an enhanced view that combines both EVM and relevant Cosmos transactions. These endpoints:
- Include both EVM and Cosmos transactions where relevant
- Provide additional context about the chain's state
- Support synthetic transactions for cross-chain events
- Offer more comprehensive transaction tracing
- Are recommended for applications that need a complete view of the chain

### Key Differences
1. **Transaction Coverage**
   - `eth_` endpoints: EVM transactions only
   - `sei_` endpoints: Both EVM and relevant Cosmos transactions

2. **Use Cases**
   - `eth_` endpoints: Best for pure EVM applications and Ethereum tooling
   - `sei_` endpoints: Best for applications needing full chain visibility

3. **Transaction Indices**
   - `eth_` endpoints: Index only EVM transactions
   - `sei_` endpoints: Index all transactions in sequence

## Sei_ Endpoints

Sei provides two main categories of custom endpoints: those for handling synthetic transactions and those for managing tracing failures. Each category serves a specific purpose in enhancing the EVM compatibility layer.

### 1. Synthetic Transaction Endpoints

#### Overview
These endpoints bridge the gap between Cosmos and EVM by exposing Cosmos-native events (CW20 and CW721) as EVM-compatible logs and receipts. This is particularly useful for:
- Indexing pointer contracts
- Tracking cross-chain token transfers
- Monitoring Cosmos-native contract events from EVM applications

#### Available Endpoints

##### Log Querying
- `sei_getFilterLogs`
  - Enhanced version of `eth_getFilterLogs`
  - Includes both EVM and synthetic logs
  - Useful for real-time event monitoring

- `sei_getLogs`
  - Enhanced version of `eth_getLogs`
  - Includes both EVM and synthetic logs
  - Ideal for historical event queries

##### Block Data
- `sei_getBlockByHash`
  - Enhanced version of its `eth_` counterpart
  - Include synthetic transactions in block data
  - Provide complete block information

> **Note**: For synthetic transactions, you can use `eth_getTransactionReceipt` with the synthetic transaction hash to retrieve receipt data. There is no `sei_getTransactionByReceipt`.

### 2. Tracing Failure Management Endpoints

#### Overview
Due to Sei's unique mempool implementation and the absence of transaction simulation, some transactions may fail before producing an opcode-level trace. These failures can occur due to:
- Nonce mismatches ("nonce too low" or "nonce too high")
- Insufficient funds
- Other panic conditions
- Post-admission state-transition failures that return before Create/Call (e.g. EIP-7623 floor data gas; receipts set `PreExecutionFailure`)

These transactions are included in blocks but have no meaningful EVM trace. The following endpoints help filter them out.

#### Available Endpoints

##### Transaction and Block Data
- `sei_getTransactionReceiptExcludeTraceFail`
  - Enhanced version of `eth_getTransactionReceipt`
  - Only returns receipts for successfully executed transactions
  - Helps avoid confusion with failed transactions

- `sei_getBlockByHashExcludeTraceFail`
  - Enhanced version of its `eth_` counterpart
  - Exclude transactions that failed pre-state checks
  - Provide cleaner block data

#### Best Practices
1. Use these endpoints when you need to:
   - Filter out failed transactions
   - Focus on successfully executed transactions

2. Consider using the standard `eth_` endpoints when you need to:
   - See all transactions, including failures
   - Debug specific failure cases
   - Maintain compatibility with standard Ethereum tooling

### Receipts and Logs
- For EVM‑originating transactions, synthetic events are included in both `eth_getLogs` and `eth_getTransactionReceipt`. The set of logs is identical across these endpoints for a given block/tx, and `logIndex` values are strictly increasing and consistent between them.
- For Cosmos‑originating transactions, synthetic events are not included in `eth_` methods. Use `sei_getLogs` to access Cosmos‑sourced synthetic logs.
