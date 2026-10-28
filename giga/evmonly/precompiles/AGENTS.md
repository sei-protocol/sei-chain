# EVM-only custom precompiles

Native contracts the EVM-only executor runs under OCC. Every node must reach the
same result from the same block, so each rule below is a consensus rule.

## State access
- Read and write chain state only through `ctx.State` and emit logs only through
  `ctx.Logs`; anything else is invisible to OCC conflict detection.
- Read block data only from `ctx.Block` (number, time, chain ID, fees, coinbase,
  prevrandao). Block time is Unix nanoseconds; keep times and periods in
  nanoseconds and convert to seconds only for output V2 gives in seconds.
- Read and write nothing off-chain: no files, network, environment, wall clock,
  or randomness. Return work `EndBlock` could not carry out as a failure, which
  the executor logs; a returned error fails the block.
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
