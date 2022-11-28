## Abstract
`dex` module is responsible for matching orders for registered contracts.

## Concepts
### Frequent Batch Auction
The traditional implementation of exchange logic would look something like the following:
1. User A sends an order placement transaction
2. User B sends another order placement transaction
3. User A's transaction is processed by matching it against the order book state and settle accordingly
4. User B's transaction is processed similarly

Sei's `dex` module takes a different approach:
1. User A sends an order placement transaction
2. User B sends another order placement transaction
3. User A's transaction is processed by simply adding the order to an in-memory queue, without matching
4. User B's transaction is processed similarly
5. At the end of the block that contains both transactions, the in-memory queue as a whole will be matched against the order book state

### Contract Registration
Since `dex` only provides order matching logic, product logic specific to individual protocols still needs to be defined in CosmWasm contracts. As such, `dex` offers a way to inform the protocol contracts about order placement and matching results. `dex` achieves this by requiring contracts that want to leverage `dex`'s order matching logic to explicitly register via a special transaction type `MsgRegisterContract`.

## State (KV Store)
The following prefixes are persisted in disk:
- "LongBook-value-": order book state on the long side where each entry represents a price level and can contain multiple orders at that same price.
- "ShortBook-value-": similar to the above but on the short side.
- "x-wasm-contract": contract registration information.
- "MatchResult-": match results of the most recent block.

The following prefixes are only used intrablock and are cleared before committing the block:
- "MemOrder-": orders added by transactions in the current block and will be matched against the order book states at the end of the block.
- "MemCancel-": cancellations added by transactions in the current block and will update the order book states at the end of the block.

## Hooks
A registered contract can define the following `sudo` hooks that will be called by the `dex` module at appropriate times:
- BulkOrderPlacements: informs the contract about order placements
- BulkOrderCancellations: informs the contract about order cancellations
- Settlement: informs the contract about matched orders and the settlement prices

There are two more utility hooks that a contract can define for housekeeping purposes (e.g. recalculate TWAPs):
- NewBlock
- FinalizeBlock (note that this is distinct from ABCI++'s FinalizeBlock)

## Transactions
- MsgPlaceOrders - place one or more orders against a registered contract
- MsgCancelOrders - cancel one or more orders against a registered contract
- MsgRegisterContract - register or reregister a CosmWasm contract with `dex`
