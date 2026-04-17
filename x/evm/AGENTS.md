# EVM Module Concepts

## Overview

The `x/evm` module integrates a full Ethereum Virtual Machine into the Sei Cosmos SDK chain. It enables native EVM transaction execution, bidirectional address association between Sei and EVM, and interoperability between CosmWasm (CW) and EVM smart contracts via pointer contracts.

---

## Dual Address Model

Every account can have both a Sei (bech32) address and an EVM (hex) address. The module maintains a bidirectional mapping between them.

- **Explicit association** — users can link their addresses via an Associate transaction or by signing any Cosmos/EVM transaction (the EVM address is derived from their secp256k1 public key).
- **Default (cast) addresses** — when no explicit association exists, the module falls back to a deterministic byte-cast. Cast addresses have limitations on receiving funds compared to fully associated addresses.

- **Why** - cast addresses will result in two views of the same account (e.g. two balances, etc.)

---

## EVM Execution

The module executes Ethereum transactions by bridging into go-ethereum's EVM interpreter:

1. An incoming `MsgEVMTransaction` is unpacked into a native Ethereum transaction.
2. A go-ethereum `vm.EVM` instance is created with appropriate block and transaction context.
3. Execution runs through go-ethereum's `core.StateTransition`.
4. Gas is converted between Sei and EVM units using a configurable normalizer.
5. Results (logs, state changes, receipts) are persisted back to Cosmos SDK stores.

There is no block-level gas limit enforced by the EVM module; gas limits are per-transaction.

Other modules and CosmWasm contracts can also invoke the EVM programmatically through internal call and delegate-call messages. However, only 1-hop of write-able call is allowed.

---

## Supported Transaction Types

| Type | EIP | Notes |
|------|-----|-------|
| Legacy | — | Type 0 |
| AccessList | EIP-2930 | Type 1 |
| DynamicFee | EIP-1559 | Type 2 |
| SetCode | EIP-7702 | Type 4, code delegation via authorization list |
| Associate | Sei-specific | Binds Sei and EVM addresses |

---

## Ante Handler Pipeline

EVM transactions go through a dedicated ante handler chain, separate from the standard Cosmos ante chain. A router decorator at the front decides which chain to use based on whether the message is a `MsgEVMTransaction`.

The EVM ante chain performs, in order:

1. **Cosmos field rejection** — EVM txs must not use Cosmos-specific fields (memo, timeout, fees, etc.).
2. **Preprocessing** — unpacks the inner Ethereum tx, recovers the sender via ECDSA, populates derived metadata. Associate transactions are handled here.
3. **Address derivation** — for Cosmos-signed txs, derives the EVM address from the public key and sets up mappings.
4. **Basic validation** — init code size, non-negative value, intrinsic gas.
5. **Signature/nonce verification** — validates chain ID and nonce. CheckTx uses pending nonce logic for mempool ordering; DeliverTx requires exact match.
6. **Fee validation** — checks against base fee and minimum fee, executes gas purchase, stores any fee surplus.
7. **Gas metering** — sets the Sei gas meter using the converted EVM gas limit.

---

## StateDB Bridge

The `state` package implements go-ethereum's `vm.StateDB` interface on top of Cosmos SDK stores. This is the core bridge that lets the EVM read and write state within the Cosmos framework.

Key design choices:

- **Balance representation** — Sei uses 6-decimal `usei` while EVM expects 18-decimal wei. The StateDB converts between them, tracking the sub-usei remainder (`wei`) separately.
- **Snapshots and reverts** — uses Cosmos `CacheMultiStore` for state snapshots. A journal records every state mutation so it can be rolled back on revert.
- **Transient state** — logs, transient storage (EIP-1153), access lists, and gas refunds are held in memory per-transaction and not persisted until finalization.
- **Coinbase collection** — each transaction gets a deterministic coinbase address for collecting fee surplus, which is swept to the fee collector at end-of-block.

---

## Deferred Processing

Rather than finalizing everything during transaction execution, some work is deferred to `EndBlock`:

- **Deferred info** — each EVM tx records its bloom filter, fee surplus, and any error to a transient store.
- **End-of-block aggregation** — surplus is collected across all transactions, failed receipts are written, and block-level bloom filters are composed.

<!-- TODO: why is processing deferred rather than done inline? what are the benefits? -->

---

## Fee Management (EIP-1559)

The module implements EIP-1559-style dynamic base fee adjustment:

- At the end of each block, the base fee is adjusted based on how actual gas usage compared to a target.
- If usage exceeds the target, the base fee increases (up to a configurable cap).
- If usage is below the target, the base fee decreases (down to a configurable floor).
- Minimum and maximum fee bounds prevent extreme values.

Fee "surplus" — the difference between what the user paid and what the protocol required — is tracked per-transaction and collected into the fee collector at end-of-block.

---

## Pointer Contracts (CW ↔ ERC Interoperability)

Pointer contracts enable tokens on one VM to be accessed from the other VM. This is the primary interoperability mechanism between CosmWasm and EVM.

**EVM pointers for CW/native tokens:**
- Native Cosmos denoms get an ERC20 representation.
- CW20 tokens get an ERC20 pointer.
- CW721 NFTs get an ERC721 pointer.
- CW1155 multi-tokens get an ERC1155 pointer.

**CW wrappers for ERC tokens:**
- ERC20 tokens get a CW20 wrapper.
- ERC721 NFTs get a CW721 wrapper.
- ERC1155 multi-tokens get a CW1155 wrapper.

Pointers are versioned and can be upgraded. A reverse registry allows looking up the original token from its pointer address.

Pre-compiled bytecode for all pointer contracts is embedded in the binary under `artifacts/`.

---

## Precompiled Contracts

The module supports custom precompiled contracts (e.g., Bank, Staking, Gov, Wasmd) that expose Cosmos SDK functionality to EVM callers. Precompiles are versioned by block height, allowing tracer functionality at historical heights.

Payable precompiles suppress transfer events to avoid double-counting when value is forwarded through them.

---

## ABCI Lifecycle

### BeginBlock
- Resets per-block tracking state (transaction results, messages).

### EndBlock
1. Cleans up old transaction hashes.
2. Migrates legacy receipts (if any remain).
3. Prunes zero-value storage slots (resumable, batched).
4. Adjusts the dynamic base fee for the next block.
5. Sweeps coinbase surplus to the fee collector.
6. Aggregates deferred info: surplus, failed receipts, block bloom.

---

## Transaction Receipts and Logs

- Receipts are built from StateDB logs, bloom filters, gas usage, and contract addresses.
- Within a block, receipts live in transient storage and are flushed to a dedicated receipt store at the end.
- Block-level bloom filters are maintained for efficient log filtering. A separate EVM-only bloom excludes CW-originated logs.

### Synthetic Receipts and Logs
There are several types of synthetic receipts/logs on Sei.
- **CW->EVM** - if a Cosmos transaction calls an EVM contract, a synthetic receipt is created to carry any logs emitted on the EVM side.
- **CW Pointee** - if a Cosmos transaction calls a CosmWasm contract that has an EVM pointer, a synthetic receipt is created and synthetic logs are emitted to represent activities from EVM lens.
- **EVM->CW** - if an EVM transaction calls a CosmWasm contract that has an EVM pointer, synthetic logs are emitted and stored on the EVM transaction's receipt.

### Receipts for Failure Scenarios

Unlike Ethereum, an EVM transaction on Sei could fail before it reaches the EVM (i.e. during ante handling).
- **Nonce Mismatch** - such failures would not result in any receipt, because they involve no state change
- **Others Ante Failures** - such failures would result in a status-0 receipt, because nonce for the sender will be incremented in such cases.

---

## WASM Integration

CosmWasm contracts can interact with the EVM through two mechanisms:

- **Queries** — static EVM calls, ERC20/721/1155 token queries, address lookups.
- **Messages** — `MsgInternalEVMCall` for regular calls and `MsgInternalEVMDelegateCall` for delegate calls (restricted to whitelisted pointer contracts).

---
