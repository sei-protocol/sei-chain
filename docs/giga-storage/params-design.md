# evmonly params

Governed chain parameters for the EVM-only chain, with gov as the first
subspace.

## Model

- Params live in the storage of the params precompile (0x1013), laid out by
  `ParamsStorage.sol`: one struct per subspace at a fixed slot and one field per
  param. Arrays, if a param ever needs one, are Solidity dynamic arrays at a
  fixed slot.
- The set of params is declared in code. A param exists when its field is in
  `ParamsStorage.sol` and its row is in the Go declaration table. A change to a
  key that is not declared is rejected, as x/params rejects a key missing from
  its subspace's KeyTable, and `param` reverts for one. A new subspace or param
  ships in a binary upgrade.
- The layout only grows. A new param takes slots from its struct's `__gap`, a
  new subspace is a new state variable after the last, and a retired param
  keeps its slot. `layout_test.go` pins every slot and offset.
- Values are stored as native types:

  | V2 type | Stored as |
  |---|---|
  | Dec | `uint256`, fixed point with 18 decimal places |
  | Duration | `uint64` nanoseconds |
  | Coins | `uint256` usei; only the usei denom exists |

  Another V2 type gets a stored form when a param first needs one.

- Owners read their params from 0x1013's storage with the `params` package's
  Go readers (`ReadGov`). Gov reads `gov`; later the executor reads block and
  evm params at the start of each block. Nothing keeps a second copy.

## Declaration table

One Go row per param, in the params package, grouped by subspace:

```go
{key: keyVotingPeriod, kind: uint64Kind, at: govVotingPeriod}
```

The table drives the typed ABI and `paramKeys()`. A value must fit its Solidity
type, and each subspace has a whole-struct check run on the result of a change.
Gov's check is x/gov's validation plus:

- every period at least `MinPeriod` (1s) and at most `MaxPeriod` (2^62 ns), so
  a block time plus a period fits in 64 bits;
- the expedited voting period below the voting period;
- expedited quorum, threshold and minimum deposit above the regular ones;
- ratios at most 1;
- a positive minimum deposit whose wei value fits in 256 bits.

A param's flat key is `<V2 key>` for a V2 scalar (`MaxValidators`) and
`<V2 key>.<field>` for a field of a V2 struct-valued key
(`votingparams.voting_period`).

## ABI

Calls take and return typed ABI values only, with no JSON. The seid CLI
translates V2's ParameterChange JSON into typed changes and renders query
results the way V2 prints them.

Gov (0x1006):

```solidity
struct ParamChange { string subspace; string key; bytes value; } // key is the flat key; value is abi.encode(<stored type>)

function submitParamChangeProposal(
    string calldata title,
    string calldata description,
    bool isExpedited,
    ParamChange[] calldata changes
) external payable returns (uint64 proposalID);
```

Params (0x1013):

```solidity
struct ParamKey { string subspace; string key; string valueType; } // valueType is the Solidity type of the encoded value

function param(string calldata subspace, string calldata key) external view returns (bytes memory); // abi.encode(<stored type>)
function paramKeys() external view returns (ParamKey[] memory);                                       // from the declaration table
```

## Proposals

- At submit, every change is decoded and checked against the current params.
  The call reverts, writing nothing, when it has no changes, a change names an
  undeclared subspace or key, a value is not a 32-byte word that fits its type,
  or the result fails the whole-struct check.
- When the proposal passes, gov applies the changes in order to a copy of each
  subspace's params, runs the whole-struct check, and writes the changed fields.
  Any failure marks the proposal Failed and writes nothing.
- Changes are per field and merge onto the current params, as x/params merges
  a partial struct, so two proposals that change different fields of a
  subspace do not undo each other.
- The proposal stores `GovStorage.ParamChangeContent`, and `proposal()` returns
  its ABI encoding.
- `submitParamChangeProposal` is priced as a gov transaction; `param` and
  `paramKeys` as queries.

## Gov as the first subspace

- `GovStorage` holds no params; gov reads `ParamsStorage.gov`. Gov keeps its
  internal types (nanoseconds, wei, `uint64` decimals) and converts on read.
- `HasParams` and the genesis `SetParams` read and write `ParamsStorage.gov`.
  The `evm_governance.params` section of `autobahn.json` gives periods as Go
  duration strings (`"48h"`), converted to nanoseconds when loaded.
- Autobahn registers the params precompile next to gov when `evm_governance`
  is set. Like gov, it refuses calls until the first block writes gov's params.

## Tests

- Layout: every `ParamsStorage.sol` slot and offset, and values the compiler
  writes read back through the Go table.
- Each gov param changed through `submitParamChangeProposal`, read back through
  `param`, and in effect for later proposals; expedited; several changes in one
  proposal; a change that fails the whole-struct check at execution leaves
  params untouched.
- Rejected at submit: no changes, undeclared subspace or key, short or
  oversized values, and values that fail validation.

## Later subspaces

Each adds a struct to `ParamsStorage.sol`, rows to the table, and a reader:

| Subspace | Params | Reader |
|---|---|---|
| baseapp | `BlockParams.max_gas` (and the other BlockParams fields) | evmonlyapp, per block |
| evm | `KeyMinFeePerGas`, `KeySeiSstoreSetGasEIP2200`, later the EIP-1559 set | executor, per block |
| migration | `NumKeysToMigratePerBlock`, `MigrateCosmosEnabled` | as needed |
| staking | the staking params | native staking |
| slashing | the slashing params | native slashing |
