# EVM-only governance precompile

The governance precompile schedules software upgrades on the EVM-only chain.
It runs the Cosmos `x/gov` + `x/upgrade` software-upgrade flow as EVM calls to
a precompile at `0x0000000000000000000000000000000000001006`, the address of
`precompiles/gov`:

1. A voter submits a `SoftwareUpgrade` or `CancelSoftwareUpgrade` proposal.
2. Voters vote until the proposal's voting end time.
3. At the end of the first block whose time reaches the voting end time, the
   proposal is tallied with the `x/gov` rules. A passing upgrade becomes the
   chain's upgrade plan, replacing any earlier plan. A passing cancellation
   clears the plan.
4. At the plan's height, a binary that does not apply the named upgrade stops
   before executing the block.

The interface is [`IGov.sol`](IGov.sol). `abi.json` is compiled from it with
`make giga-gov-abi`, and CI runs `make giga-gov-abi-check` to fail if the two
drift. Every function except `coolingVent` keeps the selector, mutability, and
return types it has in `precompiles/gov/Gov.sol`, so tooling written against
the Cosmos precompile reads this one unchanged.

## Voters and parameters

The voter set and the tally parameters come from `autobahn.json` and are fixed
for the life of the chain. They are configuration, not state, so every node
must be started with the same values.

- **Voters.** Each voter is an EVM address with a weight: the address a
  validator votes from and that validator's committee weight. Only voters may
  submit proposals or vote.
- **Parameters.** `voting_period` in seconds of block time, and `quorum`,
  `threshold`, and `veto_threshold` as fractions of voting weight, with the
  `x/gov` meanings.

There are no deposits, expedited proposals, weighted votes, or text proposals.

## Calls

| Function | Who | Effect |
| --- | --- | --- |
| `submitProposal(proposalJSON)` | voter | Stores a proposal in its voting period and returns its ID. |
| `vote(proposalID, option)` | voter | Records or replaces the caller's vote on a proposal in its voting period. |
| `getVote(proposalID, voter)` | anyone | Returns a voter's vote. |
| `proposal(proposalID)` | anyone | Returns a proposal. |
| `tallyResult(proposalID)` | anyone | Returns the live tally during voting, and the final tally afterwards. |
| `params()` | anyone | Returns the tally parameters. |
| `coolingVent()` | anyone | Always reverts with "the station is not yet operational". |

Every call reverts with an `Error(string)` reason, writes nothing, and returns
unused gas to the caller when it is refused. A call is refused when:

- the selector is unknown;
- the input is not exactly the ABI encoding of the function's arguments,
  including trailing bytes or non-zero padding;
- it carries value, is a `DELEGATECALL` or `CALLCODE`, or writes from a static
  call (`coolingVent` counts as a write);
- a proposal is malformed, or a vote arrives at or after the proposal's voting
  end time.

A vote is accepted only while the block time is before the proposal's voting
end time. Proposals are ended at the end of a block, in submission order, at
most 16 per block; later ones end in the blocks that follow.

The gas of every call is fixed by its input before the call runs:

| Function | Gas |
| --- | --- |
| `submitProposal` | `10,000 + (16 + ceil(len(input) / 32)) * 22,100` |
| `vote` | `10,000 + 4 * 2,100 + 22,100` |
| `getVote`, `params` | `10,000 + 2 * 2,100` |
| `tallyResult` | `10,000 + (voters + 8) * 2,100` |
| `proposal` | `10,000 + (largest proposal's slot count) * 2,100` |
| `coolingVent` | `16,385` (`0x4001`) |
| unknown selector | `3,000` |

## Storage

All state is contract storage of the precompile account, read and written
through the EVM `StateDB` like any contract's. It is in the app hash, rolls
back with a reverted call, and is tracked by optimistic parallel execution.
There is no state outside these slots and no range read over them.

### Slot keys

Every slot key is a keccak256 hash over a label and fixed-width parts:

```
key(label, parts...) = keccak256("sei.giga.gov/" ++ label ++ parts...)
```

`++` is byte concatenation. `u64(n)` is `n` as 8 big-endian bytes, `u8(n)` is
one byte, and addresses are their 20 bytes. Each label is used by one layout,
so no two layouts share a key.

| Slot | Key | Value |
| --- | --- | --- |
| Proposal count | `key("proposal-count")` | Highest proposal ID assigned. IDs start at 1. |
| Queue head | `key("queue-head")` | Index of the oldest proposal not yet ended. |
| Queue tail | `key("queue-tail")` | Index the next proposal is queued at. |
| Queue entry | `key("queue", u64(index))` | Proposal ID queued at `index`, cleared to 0 once it ends. |
| Proposal field | `key("proposal", u64(id), u8(field))` | One field of proposal `id`; see below. |
| Vote | `key("vote", u64(id), voter)` | `voter`'s option on proposal `id`: 1 Yes, 2 Abstain, 3 No, 4 NoWithVeto. 0 is no vote. |
| Plan field | `key("plan", u8(field))` | One field of the scheduled upgrade plan; see below. |

Proposal fields:

| `field` | Name | Value |
| --- | --- | --- |
| 1 | kind | 1 software upgrade, 2 cancel software upgrade |
| 2 | status | 2 voting period, 3 passed, 4 rejected, 5 failed |
| 3 | proposer | submitting voter |
| 4 | submit time | block time of submission, Unix seconds |
| 5 | voting end time | submit time + voting period, Unix seconds |
| 6 | plan height | upgrade height, software upgrades only |
| 7-10 | final tally | Yes, Abstain, No, NoWithVeto weight, set when the proposal ends |
| 11 | title | string |
| 12 | description | string |
| 13 | plan name | string, software upgrades only |
| 14 | plan info | string, software upgrades only |

Plan fields:

| `field` | Name | Value |
| --- | --- | --- |
| 1 | height | upgrade height; 0 means no plan is scheduled |
| 2 | name | string |
| 3 | info | string |
| 4 | proposal | ID of the proposal that scheduled the plan |

A failed proposal is a passing software upgrade whose height is not above the
block that ends it; it never becomes the plan.

### Value encoding

- **Integers** are unsigned, big-endian, in the low 8 bytes of the slot.
- **Addresses** are in the low 20 bytes of the slot.
- **Strings** store their byte length as an integer in the field's slot `s`,
  and their bytes in 32-byte chunks at `key("string-chunk", s, u64(i))` for
  chunk `i`, left-aligned and zero-padded. Writing a shorter string clears the
  chunks it no longer uses.

A slot that was never written reads as zero, which is why IDs, statuses, vote
options, and plan heights start at 1.

### What each step touches

| Step | Reads | Writes |
| --- | --- | --- |
| `submitProposal` | proposal count, queue tail | proposal count, queue tail, one queue entry, the new proposal's fields |
| `vote` | proposal count, the proposal's status and voting end time | the caller's vote slot on that proposal |
| queries | the slots they return | nothing |
| End of block | queue head and tail, queue entries, voting end times, each voter's vote on an ending proposal | its final tally and status, its queue entry, queue head, and plan fields when it passes |

Votes on the same proposal write different slots, so parallel execution runs
them without conflict. Two submissions in one block both write the proposal
count and queue tail, so they run one after the other. Tallying and ending
proposals happen outside any transaction, at the end of the block.

Nothing is deleted except queue entries and plan fields, so proposals and
votes stay readable after they end.
