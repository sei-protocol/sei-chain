# EVM-only custom precompiles

Native contracts the EVM-only executor runs under OCC. Every node must reach the
same result from the same block, so each rule below is a consensus rule.

## State access
- Read and write chain state only through `ctx.State` and emit logs only through
  `ctx.Logs`; anything else is invisible to OCC conflict detection.
- Read block data only from `ctx.Block` (`Number`, `UnixNano`, `ChainID`, fees,
  `Coinbase`, `PrevRandao`). Keep times and periods in nanoseconds. `UnixNano`
  is a whole second until CON-572 lands; depend on neither that nor sub-second
  resolution.
- Read and write nothing off-chain: no files, network, environment, wall clock,
  or randomness.
- `EndBlock` returns the operations that failed, such as a passed proposal
  that could not execute, and the executor logs them. Work it defers to a later
  block is not a failure. A returned error fails the block.
- A loop whose length comes from storage stops once `ctx.State.Failed()`: reads
  after a failure, such as running out of gas, return zeros and cost nothing.
- Compiled-in constants are allowed, but changing one changes the app hash and
  needs an upgrade.

## Interface
- Arguments and results are typed ABI values: times and periods as `uint64`
  nanoseconds, amounts as `uint256` wei, ratios and weights as `uint256` scaled
  by 1e18, accounts as `address`. No JSON and no numbers as strings.
- Match V2's operations and their rules, not its wire format. The seid CLI
  translates V2's JSON and output shapes.
- Each precompile package holds its Solidity interface (e.g. `gov/Gov.sol`) next
  to the embedded `abi.json`, generated from it.
- A method in a shipped ABI keeps its selector. Its implementation may become a
  revert whose error names the call to use instead. Today the shipped ABI is
  V2's; once this chain ships, it is every ABI it has shipped. CON-567 tracks
  checking deviations.
- Reject delegatecall, reject state changes under staticcall, and reject value
  sent to a method the ABI does not mark payable.

## Concurrency and determinism
- `Run` is called concurrently from OCC workers. A `Contract` keeps no mutable
  Go state: no caches, counters, or fields written during `Run` or `EndBlock`.
- No goroutines, no map-iteration order in results, no floating point.

## Gas
- `RequiredGas` prices a call from its selector and argument bytes alone (V2's
  `DefaultGasCost` formula).
  Storage reads, writes and logs are metered by the adapter; do not price them
  into `RequiredGas`.
- `EndBlock` is not metered. An end blocker bounds its own per-block work (see
  gov's `endBlockBudget`) so a block's cost cannot grow with user input.

## Storage
- Lay out storage as solc would for the matching `docs/giga-storage/*.sol`
  contract, using `solstorage`. Changing a layout changes the app hash.
- A storage write through `ctx.State` gives an account with no nonce and no
  code a nonce of 1, so the EVM treats any account holding storage as existing.
