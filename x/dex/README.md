## Abstract

Provides fast order matching for CosmWasm contracts.

## Contents
`dex` provides order placement/cancellation/settlement capabilities for registered contracts, as well as exchange price resulting from the order matching. The implementation of the exchange logic and store writes happen at the end of each block, based on in-memory data passed on by `dex` transactions in the same block. In other words, `DeliverTx` only adds data to memory (a custom data structure, not Cosmos's cache store), and
`EndBlock` uses those in-memory data to perform batched order matching. The `EndBlock` order matching process also involves calling `Sudo` endpoints defined on the registerd contracts, as long as those endpoints conform with the expected interface, so that the registered contracts can perform custom logic based on the exchange information.

## Concepts
Contract Registration
A contract can be registered with `MsgRegisterContract` type. There are a few related concepts:
### Order-related Sudo Calls
For `dex` to perform order matching for a registered contract, it MUST define 3 `Sudo` endpoints:
- "bulk_order_placements": payload expected can be found in [x/dex/types/order_placement.go]
- "bulk_order_cancellations": payload expected can be found in [x/dex/types/order_cancellation.go]
- "settlement": payload expected can be found in [x/dex/types/settlement.go]
If any of the three endpoints is missing, or if the endpoint is ill-defined, `dex` will skip the contract's order matching.
### Asset Pair Registration
A contract may define one or more tradable pairs with `dex`. For example, a spot trading contract may define a pair with price denomination `USDC` and asset denomination `BTC`. The exact semantics for asset pair registration can be found in the `Governance` section below. A contract with no registered pair is valid - it simply won't have any trading activity in `dex`.
### Rent
A contract must deposit a certain amount of `usei` into `dex` upon registration or through subsequent top-ups. Those `usei`, also known as rent, will be consumed when the contract's `Sudo` endpoints are called based on the gas meter reading, and distributed to Sei validators. Note that if a `Sudo` endpoint fails, it would still charge rent for whatever the gas meter has already recorded before the failure happens.
### Contract Dependencies
A contract may dispatch messages to other contracts as part of its `Sudo` call responses. If that is the case, the contract must declare those other contracts as `Dependencies` in its registration. No circular dependency is allowed. `dex` will check if a dispatched message is against a declared dependency contract, and reject it if it's not declared.
## Batch Order Matching
Orders submitted via MsgPlaceOrders are aggregated at the end of a block and matched in batch.
### Sequence
TODO
### Clearing/Settlement Rules
TODO
## State

## Governance

Token pairs can be whitelisted with a contract address to the `dex` module via governance. The following is an example
proposal json to whitelist a token pair.

```json
    "dex": {
      "params": {
        "code_upload_access": {
          "permission": "Everybody"
        },
        "instantiate_default_permission": "Everybody"
      }
    },  
```

## Messages

## Events

## Parameters

## Transactions

## Queries

