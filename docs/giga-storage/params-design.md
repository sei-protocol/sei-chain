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
  its subspace's KeyTable, and `param` reverts for one; `params` returns `""`
  for one, as V2 does. A new subspace or param ships in a binary upgrade.
- The layout only grows. A new param takes slots from its struct's `__gap`, a
  new subspace is a new state variable after the last, and a retired param
  keeps its slot. `layout_test.go` pins every slot and offset.
- Values are stored as native types:

  | V2 type | Stored as |
  |---|---|
  | Dec | `uint256`, fixed point with 18 decimal places |
  | Duration | `uint64` nanoseconds |
  | Coins | `uint256` usei; only the usei denom is accepted |
  | Int | `uint256` |

  Another V2 type gets a stored form when a param first needs one.

- Owners read their params from 0x1013's storage through
  `State.GetState`. Gov reads `gov`; later the executor reads block and evm
  params at the start of each block. Nothing keeps a second copy.

## Declaration table

One Go row per param, in the params package, grouped by subspace:

```go
{key: "votingparams.voting_period", kind: durationKind, at: govVotingPeriod}
```

The table drives every path: the typed ABI, legacy JSON decoding and encoding,
and `paramKeys()`. A value must fit its Solidity type, and each subspace has a
whole-struct check run on the result of a change. Gov's is x/gov's
validation (positive periods, expedited period below the voting period,
expedited quorum, threshold and minimum deposit above the regular ones,
ratios at most 1) plus a positive minimum deposit whose wei value fits in 256
bits.

A param's flat key is `<V2 key>` for a V2 scalar (`MaxValidators`) and
`<V2 key>.<field>` for a field of a V2 struct-valued key
(`votingparams.voting_period`).

## ABI

### Typed

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

### Legacy (V2)

- `submitProposal(string)` with `"type": "ParameterChange"` and V2's
  `changes: [{subspace, key, value}]`. Each change is decoded into flat
  `ParamChange`s; a struct-valued key takes a partial JSON object and each field
  present becomes one change, as x/params merges onto the current value. The
  call reverts, writing nothing, on:
  - JSON that does not parse, or content over the size limit;
  - an undeclared subspace, key or field, or a field given twice (V2's amino
    ignores an unknown field; here it is rejected);
  - a JSON type amino would not produce: 64-bit integers, Dec and Duration as
    quoted strings, 32-bit integers bare, Coins as `[{denom, amount}]`;
  - a denom other than usei, or a value that fails validation.
- A Duration is read as amino's nanosecond string (`"259200000000000"`), or
  as a decimal with an `s`, `ms` or `ns` suffix that is a whole number of
  nanoseconds (`"259200s"`, `"1500ms"`). V2 accepts only the first form.
- `params(string subspace, string key) returns (string)` (0x1013) returns the
  value as V2's amino JSON, built from the stored fields. A struct-valued key
  returns its whole object, leaving out zero fields as amino does. An
  undeclared subspace reverts and an undeclared key returns `""`, as in V2.

## Proposals

- At submit, every change is decoded and validated against the current params.
- When the proposal passes, gov applies the changes in order to a copy of each
  subspace's params, runs the whole-struct check, and writes the changed fields.
  Any failure marks the proposal Failed and writes nothing.
- Changes are per field, so two proposals that change different fields of a
  subspace do not undo each other.
- A legacy proposal's changes are converted to per-field changes at submit, so
  both kinds store the same `GovStorage.ParamChangeContent`. The proposal
  query returns V2's `ParameterChangeProposal` JSON built from it, with
  consecutive changes to one struct-valued key grouped under that key.
- Both submit methods are priced as gov transactions; `param`, `params` and
  `paramKeys` as queries.

## Gov as the first subspace

- `GovStorage` holds no params; gov reads `ParamsStorage.gov`. Gov keeps its
  internal types (nanoseconds, wei, `uint64` decimals) and converts on read.
  Periods are at most 2^62 nanoseconds, so a block time plus a period fits in
  64 bits.
- `HasParams` and the genesis `SetParams` read and write `ParamsStorage.gov`.
  The `evm_governance.params` section of `autobahn.json` is unchanged.
- Autobahn registers the params precompile next to gov when `evm_governance`
  is set. Like gov, it refuses calls until the first block writes gov's params.

## Tests

- Layout: every `ParamsStorage.sol` slot and offset.
- Typed: each gov param changed through `submitParamChangeProposal`, read back
  through `param`, and in effect for later proposals; expedited; several
  changes in one proposal; a change that fails the whole-struct check at
  execution leaves params untouched.
- Legacy, using pacific-1 proposals as vectors:
  - #16 (`depositparams`) and #86 (`votingparams`) decode and apply, and
    `params` returns V2's JSON for them;
  - #123 (`baseapp`/`TimeoutParams`) is rejected as undeclared;
  - malformed JSON, oversized content, unknown fields, wrong JSON types and
    non-usei denoms are rejected.
- `params` for every gov key matches V2's amino encoding of the same values,
  pinned against `sei-cosmos/x/gov/types` in a test.

## Later subspaces

Each adds a struct to `ParamsStorage.sol`, rows to the table, and a reader:

| Subspace | Params | Reader |
|---|---|---|
| baseapp | `BlockParams.max_gas` (and the other BlockParams fields) | evmonlyapp, per block |
| evm | `KeyMinFeePerGas`, `KeySeiSstoreSetGasEIP2200`, later the EIP-1559 set | executor, per block |
| migration | `NumKeysToMigratePerBlock`, `MigrateCosmosEnabled` | as needed |
| staking | the staking params | native staking |
| slashing | the slashing params | native slashing |
