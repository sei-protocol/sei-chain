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
- A storage read after a failure, such as running out of gas, returns zero and
  costs nothing, so a length read after the failure is zero. A loop that read its
  bound before the failure, and is not capped by a constant, re-checks
  `ctx.State.Failed()` each iteration.
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
- Range-check every argument at the start of `Run`, before reading state, and
  revert with `precompiles.Revert` naming the argument when one is out of range.
  A list with a repeated entry is out of range. A check that needs state fails
  the call later, as a plain error.
- Solidity files declare `pragma solidity 0.8.30;`. The precompile addresses
  hold no code, and solc 0.8.10 and later skip the code check only for a call
  that returns data, so every interface method declares a return value; 0.8.31
  and later default to Osaka, whose `CLZ` this chain lacks.

## Limits
- Enforce the limits that keep the chain from locking: a state that no
  transaction can leave, so only a software upgrade recovers it, such as a
  voting period too short for any proposal to pass.
- Do not add guardrails against capture, such as a low veto threshold that lets
  a minority block every proposal. Capture is answered off-chain, for example by
  restricting inbound RPC until it is repaired.

## Concurrency and determinism
- `Run` is called concurrently from OCC workers. A `Contract` keeps no mutable
  Go state: no caches, counters, or fields written during `Run` or `EndBlock`.
- No goroutines, no map-iteration order in results, no floating point.

## Gas
- `RequiredGas` prices a call from its selector and argument bytes alone (V2's
  `DefaultGasCost` formula). Storage reads, writes and logs are metered by the
  adapter; do not price them into `RequiredGas`.
- The adapter checks input size first, before pricing or running the call, and
  fails input over 20 KiB using all the call's gas. ABI decoding can still
  allocate more than calldata gas pays for under that cap, since array entries
  may share one encoded value: a method taking an array of dynamic values bounds
  its length and range-checks it before re-encoding or storing it.
- `EndBlock` is not metered, so `ctx.State.Failed()` never reports a storage
  read failure there. It schedules its work under a budget (see gov's
  `endBlockBudget`). Each unit of that work is bounded by limits its write path
  enforces, such as gov's content size and depositor count, and by the
  validator set, so a block's cost cannot grow with user input.

## EndBlock
- `EndBlock` returns the operations that failed, and the executor logs them.
  Work deferred to a later block is not a failure. A returned error fails the
  block, so an end blocker returns one only when the block cannot be trusted.
- Never panic. A malformed stored value is an error returned up to the
  operation that read it; the executor does not recover panics.
- Each operation runs under a `ctx.State.Snapshot()`. When it returns an error,
  revert to the snapshot, so the operation writes nothing, then record the
  outcome in place: gov marks the proposal Failed, refunds its deposits when it
  can, and removes it from its queue. Wrap the reported error in
  `precompiles.ErrDegraded`.
- One operation's failure holds up no other: the next due operation in the same
  EndBlock proceeds. An operation that returns success but leaves its queue
  entry in place is failed the same way.
- Check a balance change before making it. A balance change that fails marks
  `ctx.State` failed, which reverting a snapshot does not undo: later writes are
  dropped and the block fails. Gov checks the escrow and the depositor's balance
  before releasing a deposit.
- A queue whose stored length exceeds the entries it could hold is reported each
  EndBlock and still drained.

## Storage
- Lay out storage as solc would for the matching `docs/giga-storage/*.sol`
  contract, using `solstorage`. Changing a layout changes the app hash.
- `solstorage` returns `ErrInvalidString` for a stored string that is not a
  valid encoding of at most `MaxStringLength` bytes. Lowering `MaxStringLength`
  therefore makes any longer value already stored unreadable.
- A storage write through `ctx.State` gives an account with no nonce and no
  code a nonce of 1, so the EVM treats any account holding storage as existing.
- Chain params live in the params precompile's storage (0x1013), laid out by
  `docs/giga-storage/ParamsStorage.sol` and declared in `params`. Read them
  with the `params` package's Go readers, such as `ReadGov`; do not keep a copy
  in another precompile's storage. They change only at genesis and through a
  passed gov proposal (`params.Apply`). Read
  [`docs/giga-storage/params-design.md`](../../../docs/giga-storage/params-design.md)
  before adding a subspace or a param.
