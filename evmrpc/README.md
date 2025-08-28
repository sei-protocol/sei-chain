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
- `sei_getBlockByNumber` and `sei_getBlockByHash`
  - Enhanced versions of their `eth_` counterparts
  - Include synthetic transactions in block data
  - Provide complete block information

- `sei_getBlockReceipts`
  - Enhanced version of `eth_getBlockReceipts`
  - Includes receipts for synthetic transactions
  - Maintains transaction order and relationships

> **Note**: For synthetic transactions, you can use `eth_getTransactionReceipt` with the synthetic transaction hash to retrieve receipt data. There is no `sei_getTransactionByReceipt`.

### 2. Tracing Failure Management Endpoints

#### Overview
Due to Sei's unique mempool implementation and the absence of transaction simulation, some transactions may fail pre-state checks. These failures can occur due to:
- Nonce mismatches ("nonce too low" or "nonce too high")
- Insufficient funds
- Other panic conditions

These transactions are included in blocks but not executed. The following endpoints help filter out these failed transactions.

#### Available Endpoints

##### Block Tracing
- `sei_traceBlockByNumberExcludeTraceFail`
  - Enhanced version of `debug_traceBlockByNumber`
  - Excludes transactions that failed pre-state checks
  - Provides cleaner tracing output

- `sei_traceBlockByHashExcludeTraceFail`
  - Enhanced version of `debug_traceBlockByHash`
  - Excludes transactions that failed pre-state checks
  - Useful for debugging specific blocks

##### Transaction and Block Data
- `sei_getTransactionReceiptExcludeTraceFail`
  - Enhanced version of `eth_getTransactionReceipt`
  - Only returns receipts for successfully executed transactions
  - Helps avoid confusion with failed transactions

- `sei_getBlockByNumberExcludeTraceFail` and `sei_getBlockByHashExcludeTraceFail`
  - Enhanced versions of their `eth_` counterparts
  - Exclude transactions that failed pre-state checks
  - Provide cleaner block data

#### Best Practices
1. Use these endpoints when you need to:
   - Filter out failed transactions
   - Get cleaner debugging output
   - Focus on successfully executed transactions

2. Consider using the standard `eth_` endpoints when you need to:
   - See all transactions, including failures
   - Debug specific failure cases
   - Maintain compatibility with standard Ethereum tooling

## Transaction Index Mismatches

### Overview
When querying block receipts, there is a discrepancy between the transaction indices returned by `eth_getBlockReceipts` and `sei_getBlockReceipts` endpoints. This occurs because `eth_getBlockReceipts` only includes EVM transactions, while `sei_getBlockReceipts` includes both EVM and Cosmos transactions.

### Example
Consider a block containing the following transactions in order:
```
Block Transactions:
1. EVM Transaction 1
2. Cosmos Transaction 1
3. EVM Transaction 2
```

The transaction indices will differ between endpoints:

#### eth_getBlockReceipts
Returns only EVM transactions with sequential indices:
- EVM Transaction 1 (tx index: 0)
- EVM Transaction 2 (tx index: 1)

#### sei_getBlockReceipts
Returns all transactions (both EVM and Cosmos) with sequential indices:
- EVM Transaction 1 (tx index: 0)
- Cosmos Transaction 1 (tx index: 1)
- EVM Transaction 2 (tx index: 2)

### Important Note
When working with transaction indices, be aware that:
1. The same transaction will have different indices depending on which endpoint you use
2. `eth_getBlockReceipts` indices are based only on EVM transactions
3. `sei_getBlockReceipts` indices include all transactions in the block
4. Applications should handle these differences appropriately based on which endpoint they're using

### Best Practices
- Always use the same endpoint consistently within your application
- When switching between endpoints, be sure to account for the index differences
- Consider using transaction hashes instead of indices when possible, as they remain consistent across endpoints