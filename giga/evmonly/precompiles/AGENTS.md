# EVM-only custom precompiles

Native contracts the EVM-only executor runs under OCC. Every node must reach the
same result from the same block, so each rule below is a consensus rule.

## State access
- Read and write chain state only through `ctx.State` and emit logs only through
  `ctx.Logs`; anything else is invisible to OCC conflict detection.
- Read block data only from `ctx.Block` (`Number`, `UnixNano`, `ChainID`, fees,
  `Coinbase`, `PrevRandao`). Keep times and periods in nanoseconds and convert
  to seconds only for output V2 gives in seconds. `UnixNano` is a whole second
  until CON-572 lands; depend on neither that nor sub-second resolution.
- Read and write nothing off-chain: no files, network, environment, wall clock,
  or randomness. `EndBlock` returns work it could not carry out as a failure,
  which the executor logs; a returned error fails the block.
- Compiled-in constants are allowed, but changing one changes the app hash and
  needs an upgrade.

## Concurrency and determinism
- `Run` is called concurrently from OCC workers. A `Contract` keeps no mutable
  Go state: no caches, counters, or fields written during `Run` or `EndBlock`.
- No goroutines, no map-iteration order in results, no floating point.

## Gas
- `RequiredGas` is the flat per-call cost (V2's `DefaultGasCost` formula).
  Storage reads, writes and logs are metered by the adapter; do not price them
  into `RequiredGas`.
- `EndBlock` is not metered. An end blocker bounds its own per-block work (see
  gov's `endBlockBudget`) so a block's cost cannot grow with user input.

## Storage
- Lay out storage as solc would for the matching `docs/giga-storage/*.sol`
  contract, using `solstorage`. Changing a layout changes the app hash.
- A storage write through `ctx.State` gives an account with no nonce and no
  code a nonce of 1, so the EVM treats any account holding storage as existing.
- Chain params live in the params precompile's storage (0x1013), laid out by
  `docs/giga-storage/ParamsStorage.sol` and declared in `params`. Read them
  through `params`; do not keep a copy in another precompile's storage.

## ABI
- Each precompile package holds its Solidity interface (e.g. `gov/Gov.sol`) next
  to the embedded `abi.json`, and the two describe the same ABI. A method V2
  also has keeps V2's address, selector and names; a method V2 lacks is added
  alongside, never in place of one. CON-567 tracks checking deviations from V2.
