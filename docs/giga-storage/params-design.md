# evmonly params

Governed chain parameters for the EVM-only chain, with gov as the first
subspace.

## Model

- Params live in the storage of the params precompile (0x1013), laid out by
  `docs/giga-storage/ParamsStorage.sol`: one struct per subspace at a fixed
  slot and one field per param, commented with the param's flat key. Every
  param is a type stored in one storage word; a param that needs an array needs
  a stored form and a larger execution charge (see *Proposals*) first.
- The set of params is declared in code. A param exists when its field is in
  `ParamsStorage.sol` and its row is in the Go declaration table. A change to a
  key that is not declared is rejected, as x/params rejects a key missing from
  its subspace's KeyTable, and a `param` call for one reverts naming it. A new
  subspace or param ships in a binary upgrade, whose handler writes the new
  param's value: a new field reads as zero, which fails its subspace's check,
  so until it is written every change to that subspace is rejected.
- Until a public network runs this, the layout may be rearranged outright.
  After that it only grows: a new param takes slots from its struct's `__gap`,
  a new subspace is a new state variable after the last, and a retired param
  keeps its slot. `layout_test.go` pins every slot and offset.
- Values are stored as native types:

  | V2 type | Stored as |
  |---|---|
  | Dec | `uint256`, fixed point with 18 decimal places |
  | Duration | `uint64` nanoseconds; every period, read or written, is nanoseconds |
  | Coins | `uint256` wei, a whole number of usei; only the usei denom exists |

  Another V2 type gets a stored form when a param first needs one. A value of
  any unsigned width is stored in the bytes of its slot that solc gives its
  field, so packed fields keep their neighbours. Values decode as unsigned: a
  signed type needs a decoder before a param can use it.

- Owners read their params from 0x1013's storage with the `params` package's
  Go readers (`ReadGov`). Gov reads `gov`. Nothing keeps a second copy.
  Planned readers for other subspaces are in *Later subspaces*.
- Params are written only at genesis (`WriteGov`, which refuses once gov's
  params exist) and by a passed gov proposal (`Apply`). Both check the whole
  subspace first.

## Declaration table

One Go row per param, in the params package, grouped by subspace:

```go
{key: keyVotingPeriod, kind: durationKind, at: govVotingPeriod, least: new(big.Int).SetUint64(MinChangedVotingPeriod)}
```

The table sets the type each value is decoded and stored as, and optionally
`least`, the smallest value a param change may set; genesis is not held to it.
A value must fit its Solidity type, and each subspace has a whole-struct check
run on the result of a change. Gov's check is x/gov's validation plus:

- every period at least `MinPeriod` (1s) and at most `MaxPeriod` (2^62 ns), so
  a block time plus a period fits in 64 bits;
- the expedited voting period below the voting period;
- expedited quorum, threshold and minimum deposit above the regular ones;
- quorums in 0..1 and thresholds above 0 and at most 1;
- positive minimum deposits of whole usei.
- upper bounds, which x/gov does not have, on the params that could otherwise
  leave gov unable to pass a later proposal: `voting_period` at most 30 days,
  `min_deposit` at most 1,000,000 SEI, and `quorum` and `threshold` at most 4/5.
  The expedited params need no bound of their own: a regular proposal can
  always fix them.

These bounds stop a state that only a software upgrade could leave. They do not
guard against capture: `veto_threshold` is unbounded below, so once a low one
passes, a holder of a sliver of stake can veto every later proposal, including
one raising it again. That is answered off-chain, for example by restricting
inbound RPC until it is repaired.

The check runs on genesis params (`WriteGov`) and on every param change. A
param change also may not set `voting_period` below 5 minutes
(`MinChangedVotingPeriod`), so that later proposals stay passable. Genesis may
set a shorter one, so test networks can run short voting periods. The floor
bounds a change, not the stored value: a chain that starts below it keeps its
period until a proposal sets the period itself, and then only to 5 minutes or
more. Since the expedited period must stay below the voting period, raising the
expedited period past a short genesis voting period means raising the voting
period to 5 minutes too.

A param's flat key is `<V2 key>` for a V2 scalar (`MaxValidators`) and
`<V2 key>.<field>` for a field of a V2 struct-valued key
(`votingparams.voting_period`).

## ABI

Calls take and return typed ABI values only, with no JSON. The seid CLI
translates V2's ParameterChange JSON into typed changes and renders query
results the way V2 prints them.

Gov (0x1006):

```solidity
struct ParamChange { string subspace; string key; bytes value; } // key is the flat key; value is the 32-byte abi.encode(<stored type>)

function submitParamChangeProposal(
    string calldata title,
    string calldata description,
    bool isExpedited,
    ParamChange[] calldata changes
) external payable returns (uint64 proposalID);
```

Params (0x1013):

```solidity
function param(string calldata subspace, string calldata key) external view returns (bytes memory); // abi.encode(<stored type>)
```

Each param's type is fixed by its field in `ParamsStorage.sol`; the precompile
does not report it. V2's `params(string,string)` stays in the ABI and reverts,
naming `param`.

## Proposals

- A rejected proposal is either never written or Failed, depending on where the
  check fails:
  - Gov range-checks the arguments first, reading no state. The call reverts
    with an Error(string) naming the problem, and writes nothing, when it has no
    changes, a change names an undeclared subspace or key or a param another
    change names, a value is not a 32-byte word that fits its type, or it sets
    `voting_period` below `MinChangedVotingPeriod`.
  - Gov then checks the changes against the current params. A result that fails
    the whole-struct check fails the call with no revert data, and writes
    nothing.
  - When the proposal passes, gov runs both checks again against the params
    then current and writes each change. A failure marks the proposal Failed and
    writes nothing.
- Changes are per field and merge onto the current params, as x/params merges
  a partial struct, so two proposals that change different fields of a
  subspace do not undo each other.
- Gov charges executing a passed proposal one EndBlock budget unit per word of
  its stored content. The stored content is gov's own ABI encoding of the
  decoded changes, not the submitted calldata, so its size follows from the
  changes alone. One change adds ten words when the subspace name and the key
  each fit in 32 bytes, and the title, description and struct heads add at
  least eight more. Applying a proposal reads each subspace it changes once,
  one storage word per param, and reads and writes each change's slot. The
  charge covers that work when, over the subspaces a proposal changes,
  `8 + 10 * changes >= sum of their params + 2 * changes`. With ten one-word
  params per subspace that holds for up to four subspaces in one proposal; a
  larger subspace, or a fifth, needs the charge to grow first. The same words
  also pay for reading the stored content, so the unit is coarse.
  `TestSubspacesFitTheExecutionCharge` checks the inequality for every set of
  declared subspaces.
- EndBlock reads gov's params for each proposal it settles. A change applied by
  one proposal governs every proposal settled after it in the same EndBlock,
  in queue order.
- The proposal stores `ParamChangeContent` (declared in `Gov.sol`), and
  `proposal()` returns its ABI encoding.
- `submitParamChangeProposal` is priced as a gov transaction; `param` as a
  query.

## Gov as the first subspace

- `GovStorage` holds no params; gov reads `ParamsStorage.gov`. Gov keeps its
  internal types (nanoseconds, wei, `uint64` decimals) and converts on read.
- `HasParams` and the genesis `SetParams` read and write `ParamsStorage.gov`.
  The `evm_governance.params` section of `autobahn.json` gives periods as Go
  duration strings (`"48h"`), converted to nanoseconds when loaded.
- Autobahn registers the params precompile next to gov when `evm_governance`
  is set. Like gov, it refuses calls until the first block writes gov's params.

## Tests

- Layout: every `ParamsStorage.sol` slot, offset and field type, and values the
  compiler writes read back through the Go table.
- Every gov param changed in one `submitParamChangeProposal` and read back
  through `param`; a changed voting period in effect for later proposals; a
  change that fails the whole-struct check at execution leaves params
  untouched.
- Reverted at submit: no changes, undeclared subspace or key, a repeated
  param, short or oversized values, and a voting period below the floor.
  Failed at submit without revert data: values that fail validation against
  the current params.
- Genesis may set a voting period below the floor, and `WriteGov` refuses once
  gov's params are written.
- `ParamChangeContent`'s encoding matches solc's `abi.encode`.

## Later subspaces

Planned, not decided: the expected shape, not a commitment. Each adds a struct
to `ParamsStorage.sol`, rows to the table, and a reader. A subspace of more
than ten params needs a larger execution charge first (see *Proposals*); x/evm
has eleven today.

| Subspace | Params | Reader |
|---|---|---|
| baseapp | `BlockParams.max_gas` (and the other BlockParams fields) | evmonlyapp, per block |
| evm | `KeyMinFeePerGas`, `KeySeiSstoreSetGasEIP2200`, later the EIP-1559 set | executor, per block |
| migration | `NumKeysToMigratePerBlock`, `MigrateCosmosEnabled` | as needed |
| staking | the staking params | native staking |
| slashing | the slashing params | native slashing |
