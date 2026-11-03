# Giga staking, distribution and slashing: storage proposal

This is a proposal for how state for the EVM-only staking (`0x1005`),
distribution (`0x1007`) and slashing (`0x1014`) precompiles could be stored. It
is not a settled design. It starts from three principles and works forward, so
each choice can be traced back to the principle behind it, and it calls out the
places where the principles pull against each other and the choice is still
open.

The draft layouts are the `.sol` files in this directory: `StakingStorage.sol`,
`DistributionStorage.sol`, `SlashingStorage.sol`, and the shared `Sets.sol`.
`staking-design.md` is a terser working draft of the same proposal. Nothing here
is implemented.

The three principles:

1. **Storage follows Solidity's layout rules**, ideally declared in a `.sol`
   file.
2. **Migration from V2 should be easy**, so the proposal copies V2's record
   structures wherever it can, while using Ethereum-native data types. V2's
   precompiles would be shut off before the migration and the giga precompiles
   turned on after it.
3. **How staking rewards are applied during execution is not designed yet.**
   The storage should be able to support that design without presuming it.

---

## Principle 1: storage follows Solidity's layout rules

### What it means

Each precompile would keep its state as the contract storage of its own
address, as a deployed contract does. The slot for every value is the slot the
Solidity compiler would assign if the `.sol` file in this directory were
compiled. Go code reads and writes those slots through the `solstorage`
helpers, the same way gov and params do today.

Solidity's rules, briefly:

- State variables take slots in declaration order, starting at 0.
- Small fields (`bool`, `uint8`, `uint64`, `address`) are packed into one
  32-byte slot when they fit next to each other.
- A struct starts on a fresh slot and takes consecutive slots for its fields.
- A static array (`uint256[8]`) is laid out inline, like a struct.
- A dynamic array keeps its length in its own slot; its elements start at
  `keccak256(slot)`.
- A mapping's own slot stays empty; the value for key `k` lives at
  `keccak256(k . slot)`. Nested mappings apply this once per level.
- `string` and `bytes` are stored inline when short and spill out to
  `keccak256(slot)` when long.

For example, a delegation's shares would live at
`delegations[delegator][validator].shares`. In the draft, `delegations` sits at
slot 7 of `StakingStorage` (the set before it takes two slots), so the shares
are at `keccak256(validator . keccak256(delegator . 7))`, with each key padded
to 32 bytes.

`Validator` shows the struct rules on a larger record. In the draft it takes 23
slots:

| Slot offset | Fields |
|---|---|
| 0 | `consensusPubkey` |
| 1 | `jailed`, `status` (packed) |
| 2 | `tokens` |
| 3 | `delegatorShares` |
| 4–8 | `description` (five strings) |
| 9 | `unbondingHeight`, `unbondingTime` (packed) |
| 10–13 | `commission` (three rates, then `updateTime`) |
| 14 | `minSelfDelegation` |
| 15–22 | `__gap` (reserved) |

### Why

- **The `.sol` file is the spec.** `solc --storage-layout` or `forge inspect`
  on it gives the exact slot of every field, so there is no second,
  hand-written description of the storage to drift out of date.
- **Standard tooling can read the state.** `eth_getStorageAt`, explorers and
  debuggers already understand this layout, and a contract could be written
  against the same file to read it.
- **It matches gov and params**, so all giga precompiles would share one
  storage model and one set of helpers.

### Consequences

**Layouts only grow.** Once a public network runs a layout, a state variable or
field cannot move. New fields would take slots from a struct's `__gap`, and new
state variables would go after the last one. The draft `Validator` reserves
eight slots for this, and `layout_test.go` pins would catch an accidental
reorder.

**No library dependencies.** The draft layouts import nothing from outside this
directory, including OpenZeppelin. The precompiles need enumerable sets, and
OpenZeppelin's `EnumerableSet` has the right shape, but importing it would tie
the storage to a third-party version. `Sets.sol` instead declares `AddressSet`
and `UintSet` as plain structs with the same slot layout:

```solidity
struct AddressSet {
    address[] values;                       // members, in insertion order
    mapping(address => uint256) positions;  // 1-based index into values; 0 = absent
}
```

Removing a member moves the last value into the gap, so each operation is a
constant number of writes. `solstorage.Set` already reads this layout.

**Some things IAVL can do, slots cannot.** V2 stores state in an ordered key
value store. It can walk keys in sorted order, walk every key under a prefix,
delete every key under a prefix, and ask whether any key under a prefix exists.
Contract storage is a hash map with none of those operations. Most places this
proposal departs from V2's structures come from that gap. The largest, the
prefix walks, is answered by keeping only the walks an on-chain path needs; see
below.

---

## Principle 2: copy V2's structures

### What it means

The migration from V2 is likely the riskiest step in this work. It moves the
stake and pending rewards of every delegator on the chain, and it has to finish
while blocks keep coming. One way to keep it manageable is to make it close to
a transcription: each V2 record becomes one giga record with the same shape, so
the migration code is a field-by-field conversion and a test can compare V2 and
giga record for record.

The proposal copies **structure**, not data types:

- **Each V2 store prefix that an on-chain path reads becomes one state
  variable**, named after the V2 record and commented with the prefix it came
  from. Indexes that only queries or entry slashing read are left out; see
  *Prefix walks*.
- **Each record keeps V2's fields, field order and names.**
- **Data types are Ethereum-native.** Amounts are wei, not usei. Shares, rates
  and reward ratios are 1e18-scaled fixed point, which is also V2's `sdk.Dec`
  scale. Addresses are EVM addresses, times are Unix nanoseconds.
- **The rewards accounting stays F1.**

**Converting amounts to wei.** One usei is 10¹² wei, so the migration would
multiply token amounts by 10¹². To keep each validator's exchange rate between
tokens and shares unchanged, shares would be multiplied by the same factor, as
would delegations' starting stake. F1's reward ratios are rewards per token, so
they are unchanged when both sides scale. V2 keeps rewards as `Dec` because usei
is coarse. In wei the fractional part is far smaller; one option is to store
reward and pool amounts as whole wei and drop the sub-wei remainder (under one
wei per record) at migration, another is to keep them 1e18-scaled as V2 does.
This needs a decision.

**Why F1.** V2 does not credit each delegator every block. It credits each
*validator* with a running "rewards per token" total, and records, for each
delegation, the period of that total it started in. When a delegator acts, their
rewards are `stake × (current total − total when they started)`. Nothing
per-delegation is written until the delegator does something. A simpler
rewards-per-share model was considered, but it would mean settling every
delegation's pending rewards at cutover, which with something on the order of a
million delegations is not a single-block operation. Keeping F1 lets pending
rewards carry over in the same shape.

### The mapping

Staking:

| V2 prefix | V2 record | Proposed giga state variable |
|---|---|---|
| `0x11` | last validator power | `lastValidators` (array, one slot per bonded validator) |
| `0x12` | last total power | `lastTotalPower` |
| `0x21` | validators | `validators` (+ set) |
| `0x22` | validator by consensus address | `validatorsByConsensusPubkey`, keyed by the key itself |
| `0x23` | validators by power | `validatorsByPower` (sorted array) |
| `0x31` | delegations | `delegations` (+ set per delegator) |
| `0x37` | delegations by validator | not carried over (queries only) |
| `0x32`, `0x33` | unbonding delegations and their index | `unbondingDelegations`; the index is not carried over |
| `0x34`–`0x36` | redelegations and their indexes | `redelegationEntryCounts` and `redelegationsByValDst`; entries live in the redelegation queue, and `0x35` is not carried over |
| `0x41`–`0x43` | unbonding, redelegation, validator queues | three queues |
| `0x50` | historical info | not carried over (see below) |

Distribution: prefixes `0x00`–`0x08` map one to one onto the nine state
variables in `DistributionStorage.sol`, from `communityPool` to
`validatorSlashEvents`.

Slashing: signing info (`0x01`) and Sei's packed missed-block array (`0x02`)
map one to one, keyed by operator rather than consensus address. The missed-block
`uint64[]` packs four words per slot, so the bits are V2's bits. The
address-to-pubkey relation (`0x03`) is not carried over, since the key is on the
staking `Validator`.

### The power index

V2 keeps validators sorted by power under `0x23`, and EndBlock walks that range
from the top to pick the active set. The proposal keeps the same structure: a
stored index, `validatorsByPower`, holding the same validators V2 indexes
(jailed validators are left out), in V2's order: descending by consensus power,
ties to the lower address. Because it is committed state, the index is the same
on every node, survives restarts and state sync without a rebuild, and can be
copied from V2's `0x23` range in order at cutover.

Slots cannot keep keys sorted for free, so the index is an `address[]` that the
precompile keeps in order. When a validator's power changes, its old position is
found by binary search over the array (reading each probed validator's power),
and it moves up or down until it is in order again, one slot write for each
place it passes. A change that does not pass a neighbour costs reads only, since
the validator's tokens are written anyway.

**How often that happens.** Over about 28 hours of pacific-1 (216,000 blocks,
ending at height 241,156,163), the 40 bonded validators saw 60 delegations, 29
undelegations and 2 incoming redelegations, about one power change every 2,400
blocks, touching 23 validators. The gaps between neighbours in the ranking are
large: the smallest gap between adjacent bonded validators is about 225k SEI,
the median 1.9M SEI, and 40th place sits 27.8M SEI above 41st. Almost every
change moves its validator zero places; a full re-sort never happens.

**The worst case and whether it can be abused.** The most a single transaction
can cost is moving one validator from last to first: about N slot writes, where
N is the number of indexed validators (88 today). The precompile would charge
gas for each place moved, so whoever causes a reorder pays for it, and the cost
of a transaction is bounded by N.

Flipping a validator between first and last repeatedly is not a cheap way to
load the chain:

- Moving from 41st to 1st today needs more than the top validator's stake,
  about 183M SEI on top of the 17.6M already there. Even a one-place move needs
  at least the smallest gap, about 225k SEI.
- Moving the stake back is rate-limited. Undelegating locks funds for 21 days.
  A redelegation's destination cannot be redelegated out of until that entry
  matures, and `maxEntries` caps open entries at 7 per pair. Each tranche of
  capital gets roughly two large moves per unbonding period.
- The capital is not lost; it stays staked and earns rewards. The limit on the
  attacker is the capital needed and the lock-ups, and the cost is the gas,
  which is no worse than a contract writing N slots.

A cheaper variant would be to register many validators with small, nearly equal
stakes, so that small delegations cause long moves. That is bounded by a cap on
validator records (a `maxValidatorRecords` param) and a minimum self-delegation
to create a validator, together with the per-place gas charge. V2's index has
the same economics for active-set churn; the difference is that V2 pays a fixed
IAVL delete and insert, where slots pay per place moved.

**Scaling.** At tens of thousands of validator records, gaps shrink and changes
move further, so the per-place cost grows. The cap keeps N in the range where
that cost is small.

**Alternatives considered:**

- **Rank in memory at EndBlock.** Read every validator, sort, take the top
  `maxValidators`. Nothing extra is stored, but every validator is read every
  block.
- **A node-local cache.** The same sorted list held in memory instead of slots.
  The reorder is not gas-metered, and the cache must be rebuilt identically on
  every node, follow tx reverts and snapshots, and be rebuilt after state sync.
- **Ordered flatkv keys**, V2's approach exactly. See *Prefix walks*.

### Where the structures don't carry over

Each of these is a place where slot storage, or a deliberate simplification,
forces a change from V2. The first is the hardest.

#### Prefix walks

Anywhere V2 walks every key under a prefix ("all delegations of this delegator",
"all delegators of this validator", "all validators"), slot storage needs an
explicit enumeration set, and each set adds two or three slot writes whenever a
member joins or leaves. The draft keeps two:

- `delegationsByDelegator`, which gov's tally walks for each voter.
- `validatorSet`, every validator record. Nothing on chain walks it while
  `validatorsByPower` holds the unjailed validators, so whether it stays is
  open.

V2's other walks have no on-chain reader in giga:

| V2 walk | Read by |
|---|---|
| `0x37`, delegations by validator | queries |
| `0x33`, unbonding delegations by validator | slashing unbonding entries |
| `0x35`, redelegations by source validator | slashing redelegation entries |
| unbonding delegations and redelegations by delegator | queries |

Queries would be served by an off-chain indexer reading the precompile's logs.
Slashing unbonding and redelegation entries is deferred (see *Not yet covered*).
Both kinds of entry keep their creation height and amounts, so turning it on
needs only a way to find a validator's entries: these walks back, or a check
made as each entry matures.

Ordered flatkv keys, V2's approach on IAVL, are the alternative for the walks
that remain and for the power index. They would give back ordered and prefix
iteration, prefix delete and existence checks, but give up Principle 1 for
those records: they would not be described by a `.sol` file or readable through
`eth_getStorageAt`. They also depend on flatkv exposing ordered prefix
iteration to the precompiles, as part of committed state.

#### Smaller changes

**"Does any key exist under this prefix" becomes a counter.** V2 refuses to
chain redelegations by checking whether any key exists under
`0x36 ‖ dst ‖ delegator`. The draft counts open entries in
`redelegationsByValDst` instead.

**Redelegation entries live in the queue.** On chain, V2 reads a redelegation
entry's fields only to slash it. Completing one moves no funds, since the stake
moved when the redelegation began. The draft keeps each entry's fields in its
`redelegationQueue` entry, and counts open entries per delegator, source and
destination in `redelegationEntryCounts` for `maxEntries`. A matured entry pops
off the queue and decrements that count and `redelegationsByValDst`.

**Time-ordered queues become first-in, first-out lists.** V2 keys its maturity
queues by completion time and walks them up to the current time. The draft
makes each queue an array read from its head, with each new entry's completion
time clamped to be no earlier than the entry before it. Two kinds of entry are
clamped:

- A redelegation from an unbonding validator. V2 completes it when that
  validator finishes unbonding. The clamp moves it to the completion time of the
  entry queued before it, up to one unbonding period from now. The stake moves
  at once either way, so the entry only holds the delegator to the chaining rule
  and `maxEntries` for longer, by at most the time since the source began
  unbonding.
- Every new entry after governance shortens `unbondingTime`, which waits behind
  older ones rather than overtaking them.

A validator that re-bonds before its unbonding finishes would leave a stale
queue entry, which EndBlock skips. Ordered flatkv keys would avoid the clamp.

**Slash events become an array ordered by height**, found by binary search,
where V2 uses a key range by height.

**No prefix delete.** When a validator is removed, V2 deletes its whole
historical-rewards prefix. The draft deletes the periods that can still be
referenced (the slash events' periods and the last period); F1's reference
counts mean no others are live.

**One denomination.** V2's rewards are `DecCoins`, a list of amounts by denom.
Multi-denom rewards are going away, so each amount is a single `uint256` of wei.
On pacific-1 the community pool and the outstanding rewards sampled are usei
only today; the pre-migration checklist would confirm it for everything.

**Typed keys.** Bech32 strings become EVM addresses, and the consensus public key
(a protobuf `Any` in V2) becomes a `bytes32` ed25519 key. All 88 validators on
pacific-1 use ed25519.

**No consensus address.** Staking, distribution and slashing key a validator by
its operator. The one lookup by key, `validatorsByConsensusPubkey`, rejects a
key another validator already uses, as V2's `0x22` does, and resolves a key that
consensus names; autobahn names committee members by key. Keying validators by
an address derived from the key would make the duplicate check free, but nobody
could sign for that address, so a validator would still need a separate
operator, and could never change its key.

**Historical info is not carried over.** V2 writes the full validator set into
`0x50` every block, and its only reader is IBC, which giga does not run.

### A consequence for gov

V2 tallies a proposal inside one EndBlock, so it sees state at one instant. Gov
on giga counts votes under a per-block budget, so a proposal with many voters is
counted over several blocks, and a delegation that changes mid-count could be
counted on both sides.

One way to handle this without giving staking a history is to freeze the inputs
while gov counts:

- staking refuses delegate, undelegate and redelegate while a tally is being
  counted, with a revert naming the tally;
- gov stores the bonded validator set (at most `maxValidators` records) when
  counting starts, since jailing can change the set without any delegation
  write.

That would reproduce V2's single-instant tally. The lock would last a few blocks
for a typical proposal and about two minutes for one with a million voters. Both
are small changes to gov's existing tally code.

### Principle 2.1: precompiles are off during the migration

V2's staking, distribution and slashing precompiles would be shut down ahead of
the migration, and the giga precompiles turned on only once it finishes. In
between, no user transaction can touch this state.

That keeps the migration simple. If nothing writes a delegation while it runs, a
V2 record has been migrated exactly when it lies behind the migration cursor.
There is no promote-on-write path, no "already migrated" marker, and no conflict
between a user's write and the copy.

A possible sequence:

1. **At the cutover height, copy everything per validator in one go**:
   validator records, power, the validator queue, every per-validator rewards
   record, slash events, the community pool, signing info and missed blocks,
   params and escrow balances. This is about a hundred validators' worth of
   state.
2. **Sweep the per-delegation state over the following blocks.** Staking's
   EndBlock copies V2 records in V2 key order, a budgeted number per block, and
   saves its position in `migrationCursor`. Delegations first, each with its F1
   starting info, the historical rewards entry it references, and the
   delegator's withdraw address; then unbonding entries, redelegation entries
   and the two remaining queues. A record that cannot be copied goes into
   `migrationFailures` and is skipped, so one bad record does not stall the
   sweep.
3. **Finish.** When the cursor passes the last V2 key, `migrating` clears, V2
   state is no longer read, and the giga precompiles turn on.

Sei has done something similar in V2: the `0x37` delegation-by-validator index
was backfilled with a cursor key and a ready marker.

**How long it might take.** Roughly V2 keys × writes per key ÷ the per-block
budget. V2 holds about three or four keys per delegation, and each takes at
most three or four slot writes; the index keys giga does not carry over
(`0x33`, `0x35`, `0x37`) take none. At a million delegations and 10,000 keys per
block, that is about 1,500 blocks, or ten minutes at 400 ms blocks; at 100,000
delegations, about a minute. The real delegation count is not known yet.

**Where V2 state is read from.** The sweep would read V2 through an interface
implemented in `evmonlyapp`, which may import sei-cosmos, so `giga/evmonly`
only sees typed records. Every node needs the frozen V2 state as of the cutover
for the whole sweep, including nodes that state-sync during it. If the existing
memiavl-to-flatkv migration has moved staking and distribution into flatkv by
then, that store may serve as the source.

**What still runs during the migration.** Turning the precompiles off stops
user writes, but some work is driven by the chain itself:

- Unbonding and redelegation maturity could pause, with entries that come due
  paid on the first block after the migration.
- Gov could defer any tally that would start during the migration.
- Whether the validator set can change during the window (jailing for downtime,
  validators finishing their unbonding) is open. The per-validator records are
  migrated first, so it is possible, but freezing the set is simpler.
- Whether rewards accrue during the window depends on Principle 3.

---

## Principle 3: applying rewards during execution is not designed yet

How fees and inflation are collected and credited each block is a separate
design. This proposal does not try to answer it; it aims to keep the storage
able to carry whatever that design decides, and lists the assumptions it would
need to confirm.

**What the storage would support.** With F1, crediting rewards only touches
per-validator records: the validator's outstanding rewards, current rewards and
commission. Distribution could expose two Go entry points for that:

- `AllocateTokensToValidator(state, from, validator, amount)`: moves funds into
  the distribution escrow and splits commission as V2 does;
- `FundCommunityPool(state, from, amount)`.

The rewards code would call these and write no distribution slots directly.

**What it would not support cheaply.** Anything that writes to every delegation
on a schedule. With F1, a delegation is written only when its owner acts. A
design that pushes rewards to each delegator every epoch would be on the order
of a million state writes per epoch.

**Assumptions for the rewards design to confirm or replace:**

- Rewards are allocated in a new BeginBlock step, before slashing and before any
  transaction, using the previous block's votes. The giga executor has no
  BeginBlock hook today.
- The fee pot is for the rewards design to define. Giga's coinbase and base fee
  are zero today.
- Minting belongs to the rewards design. pacific-1's release schedule is empty.
- Power and the bonded set are read from staking, not copied.
- `communityTax` and the proposer reward params stay in distribution's params
  (all zero on pacific-1).

---

## How the pieces could fit together

**Funds in escrow addresses.** The tokens would be the balances of derived
addresses, the pattern gov uses for deposits:

| Escrow | V2 module account | Holds |
|---|---|---|
| staking bonded escrow | `bonded_tokens_pool` | bonded validators' tokens |
| staking not-bonded escrow | `not_bonded_tokens_pool` | unbonded validators' tokens and unbonding entries |
| distribution escrow | `distribution` | outstanding rewards and the community pool |

Escrows and records both hold wei. Each payout would check the escrow balance
before moving funds.

**Precompiles call each other in Go.** Staking would define a hooks interface
equivalent to V2's `StakingHooks`, and `evmonlyapp` would wire distribution and
slashing into it. Gov would read staking through an adapter, so no precompile
package imports another.

**Block order follows V2.** BeginBlock would run rewards, then slashing.
EndBlock would run gov, then staking: the migration sweep while it is running,
then the validator set update, then the three queues. Each EndBlock step would
follow gov's fail-safe rule: each item runs in its own snapshot, a failure
reverts that item and is reported, and the block does not halt. Two guards
would sit where every path passes: the validator set update never sends a set
with zero total power, and a payout that finds its escrow short leaves the
funds where they are and reports it.

---

## Checklist before the migration

Proposed steps on pacific-1 before a cutover height is chosen.

1. **Confirm there are no CosmWasm delegators.** The proposal keys everything by
   EVM address. A CosmWasm contract has a 32-byte address with no EVM
   equivalent, so its delegations would have no owner on giga. Walk every
   delegator in `0x31`, `0x32` and `0x34` and flag any 32-byte address. The same
   walk can flag multisig and module accounts, and any account that has never
   signed a transaction (no public key in x/auth, so no derivable EVM address).
   Each one found needs a decision: undelegate it before cutover, or map it some
   other way.
2. **Confirm every reward balance is usei.** Multi-denom rewards are going away,
   and the proposal stores a single amount. Check the community pool and every
   validator's outstanding rewards and accumulated commission. If another denom
   is present, one fix is to force a distribution before cutover: withdraw all
   delegator rewards and validator commission so non-usei amounts are paid out
   in V2, leaving only usei to migrate.
3. **Get a real delegation count, then shadow-migrate.** The count sets how long
   the sweep runs and how much the set overhead matters. Count `0x31` keys on a
   pacific-1 snapshot, then run the migration against snapshots, more than once,
   before doing it for real. Each shadow run could report how many blocks the
   sweep took, any entries in `migrationFailures`, and these invariants:
   - each validator's `delegatorShares` equals the sum of its delegations'
     shares;
   - the bonded escrow equals the total tokens of bonded validators, and the
     not-bonded escrow equals unbonded validators' tokens plus every unbonding
     balance;
   - the distribution escrow covers every outstanding reward plus the community
     pool;
   - a sample of delegators' pending rewards computed on giga matches V2, ×10¹².

---

## Not yet covered

Known gaps that need an owner or a decision.

- **Vesting accounts.** V2 tracks delegated vesting and delegated free amounts
  on vesting accounts. Giga has no vesting accounts; what happens when a
  vesting delegator undelegates is undecided.
- **Bank balances and escrow funding** at cutover belong to the bank migration.
- **Dynamic validator sets in autobahn.** The giga application returns the
  genesis validator set today, and validator updates have nowhere to go.
- **Validator operations through authz.** Some operators run validator
  operations through authz grants, which the giga precompiles would not
  support.
- **Signing state at cutover.** Whether missed-block windows, jail times and
  tombstones copy as they are, or liveness windows restart, is undecided.
- **Slashing stake.** Both slash fractions are zero on pacific-1. The proposal
  pins them at zero and defers slashing unbonding and redelegation entries.
- **Existing callers of the V2 precompiles.** Contracts calling the V2
  selectors would get a revert naming the method to use instead.
- **Events.** Indexers and explorers would move from Cosmos events to EVM logs.
- **Parallel execution.** The escrows, `lastTotalPower` and the validator
  records make staking transactions conflict with each other. Custom
  precompiles run sequentially today.
- **In-flight gov proposals** at cutover belong to gov's migration.

## Open questions

1. Power index: what cap on validator records, and what minimum to create
   one? And is gas per place moved the right charge for a reorder?
2. Prefix walks: does `validatorSet` stay? And do the remaining set and the
   power index stay in slots, or move to ordered flatkv keys?
3. Reward and pool amounts: whole wei, or 1e18-scaled wei?
4. Is the clamped queue behaviour acceptable: a redelegation from an unbonding
   validator held to the chaining rule for longer, and new entries waiting
   behind older ones after a shortened `unbondingTime`?
5. Do the gov changes (counting lock and stored bonded set) land in the gov PR
   stack or with the staking work, and is a tally lock of a couple of minutes
   acceptable for very large proposals?
6. Is the validator set frozen during the migration, or can it change?
7. Who owns the V2 read side of the sweep, and can the flatkv copy serve it?
