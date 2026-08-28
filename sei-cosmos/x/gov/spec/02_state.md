<!--
order: 2
-->

# State

## Parameters and base types

`Parameters` define the rules according to which votes are run. There can only
be one active parameter set at any given time. If governance wants to change a
parameter set, either to modify a value or add/remove a parameter field, a new
parameter set has to be created and the previous one rendered inactive.

### DepositParams

+++ https://github.com/cosmos/cosmos-sdk/blob/v0.40.0/proto/cosmos/gov/v1beta1/gov.proto#L127-L145

### VotingParams

+++ https://github.com/cosmos/cosmos-sdk/blob/v0.40.0/proto/cosmos/gov/v1beta1/gov.proto#L147-L156

### TallyParams

+++ https://github.com/cosmos/cosmos-sdk/blob/v0.40.0/proto/cosmos/gov/v1beta1/gov.proto#L158-L183

Parameters are stored in a global `GlobalParams` KVStore.

Additionally, we introduce some basic types:

```go
type Vote byte

const (
    VoteYes         = 0x1
    VoteNo          = 0x2
    VoteNoWithVeto  = 0x3
    VoteAbstain     = 0x4
)

type ProposalType  string

const (
    ProposalTypePlainText       = "Text"
    ProposalTypeSoftwareUpgrade = "SoftwareUpgrade"
)

type ProposalStatus byte


const (
	StatusNil           ProposalStatus = 0x00
    StatusDepositPeriod ProposalStatus = 0x01  // Proposal is submitted. Participants can deposit on it but not vote
    StatusVotingPeriod  ProposalStatus = 0x02  // MinDeposit is reached, participants can vote
    StatusPassed        ProposalStatus = 0x03  // Proposal passed and successfully executed
    StatusRejected      ProposalStatus = 0x04  // Proposal has been rejected
    StatusFailed        ProposalStatus = 0x05  // Proposal passed but failed execution
)
```

## Deposit

+++ https://github.com/cosmos/cosmos-sdk/blob/v0.40.0/proto/cosmos/gov/v1beta1/gov.proto#L43-L53

## ValidatorGovInfo

This type is used in a temp map when tallying

```go
  type ValidatorGovInfo struct {
    Minus     sdk.Dec
    Vote      Vote
  }
```

## Proposals

`Proposal` objects are used to account votes and generally track the proposal's state. They contain `Content` which denotes
what this proposal is about, and other fields, which are the mutable state of
the governance process.

+++ https://github.com/cosmos/cosmos-sdk/blob/v0.40.0/proto/cosmos/gov/v1beta1/gov.proto#L55-L77

```go
type Content interface {
	GetTitle() string
	GetDescription() string
	ProposalRoute() string
	ProposalType() string
	ValidateBasic() sdk.Error
	String() string
}
```

The `Content` on a proposal is an interface which contains the information about
the `Proposal` such as the tile, description, and any notable changes. Also, this
`Content` type can by implemented by any module. The `Content`'s `ProposalRoute`
returns a string which must be used to route the `Content`'s `Handler` in the
governance keeper. This allows the governance keeper to execute proposal logic
implemented by any module. If a proposal passes, the handler is executed. Only
if the handler is successful does the state get persisted and the proposal finally
passes. Otherwise, the proposal is rejected.

```go
type Handler func(ctx sdk.Context, content Content) sdk.Error
```

The `Handler` is responsible for actually executing the proposal and processing
any state changes specified by the proposal. It is executed only if a proposal
passes during `EndBlock`.

We also mention a method to update the tally for a given proposal:

```go
  func (proposal Proposal) updateTally(vote byte, amount sdk.Dec)
```

## Stores

_Stores are KVStores in the multi-store. The key to find the store is the first
parameter in the list_`

We will use one KVStore `Governance` to store three mappings:

- A mapping from `proposalID|'proposal'` to `Proposal`.
- A mapping from `proposalID|'addresses'|address` to `Vote`. This mapping allows
  us to query all addresses that voted on the proposal along with their vote by
  doing a range query on `proposalID:addresses`.
- A mapping from `proposalID|'delegations'|address` to the voter's per-validator
  delegation shares, maintained until tallying starts.

For pseudocode purposes, here are the two function we will use to read or write in stores:

- `load(StoreKey, Key)`: Retrieve item stored at key `Key` in store found at key `StoreKey` in the multistore
- `store(StoreKey, Key, value)`: Write value `Value` at key `Key` in store found at key `StoreKey` in the multistore

## Proposal Processing Queue

**Store:**

- `ProposalProcessingQueue`: A queue `queue[proposalID]` containing all the
  `ProposalIDs` of proposals that reached `MinDeposit`. During each `EndBlock`,
  proposals that have reached the end of their voting period are advanced within
  the block's vote-processing budget.

To process a finished proposal, the application tallies the votes, computes the
votes of each validator and checks if every validator in the validator set has
voted. If the proposal is accepted, deposits are refunded. Finally, the proposal
content `Handler` is executed.

Expired proposals remain queue-ordered. If an earlier proposal does not finish
within the block's vote-processing budget, the queue scan stops and later proposals
wait for the earlier tally to complete. This also prevents the block from initializing
validator snapshots for an unbounded number of proposals after the vote budget is
exhausted.

And the pseudocode for the `ProposalProcessingQueue`:

```go
  in EndBlock do

    for finishedProposalID in GetAllFinishedProposalIDs(block.Time)
      proposal = load(Governance, <proposalID|'proposal'>) // proposal is a const key

      validators = Keeper.getAllValidators()
      tmpValMap := map(sdk.AccAddress)ValidatorGovInfo

      // Initiate mapping at 0. This is the amount of shares of the validator's vote that will be overridden by their delegator's votes
      for each validator in validators
        tmpValMap(validator.OperatorAddr).Minus = 0

      // Tally
      voterIterator = rangeQuery(Governance, <proposalID|'addresses'>) //return all the addresses that voted on the proposal
      for each (voterAddress, vote) in voterIterator
        delegations = getVoteDelegationSnapshot(voterAddress)

        for each delegation in delegations
          // make sure delegation.Shares does NOT include shares being unbonded
          tmpValMap(delegation.ValidatorAddr).Minus += delegation.Shares
          proposal.updateTally(vote, delegation.Shares)

        _, isVal = stakingKeeper.getValidator(voterAddress)
        if (isVal)
          tmpValMap(voterAddress).Vote = vote

      tallyingParam = load(GlobalParams, 'TallyingParam')

      // Update tally if validator voted they voted
      for each validator in validators
        if tmpValMap(validator).HasVoted
          proposal.updateTally(tmpValMap(validator).Vote, (validator.TotalShares - tmpValMap(validator).Minus))



      // Check if proposal is accepted or rejected
      totalNonAbstain := proposal.YesVotes + proposal.NoVotes + proposal.NoWithVetoVotes
      if (proposal.Votes.YesVotes/totalNonAbstain > tallyingParam.Threshold AND proposal.Votes.NoWithVetoVotes/totalNonAbstain  < tallyingParam.Veto)
        //  proposal was accepted at the end of the voting period
        //  refund deposits (non-voters already punished)
        for each (amount, depositor) in proposal.Deposits
          depositor.AtomBalance += amount

        stateWriter, err := proposal.Handler()
        if err != nil
            // proposal passed but failed during state execution
            proposal.CurrentStatus = ProposalStatusFailed
         else
            // proposal pass and state is persisted
            proposal.CurrentStatus = ProposalStatusAccepted
            stateWriter.save()
      else
        // proposal was rejected
        proposal.CurrentStatus = ProposalStatusRejected

      store(Governance, <proposalID|'proposal'>, proposal)
```

## Incremental tally state

An expired proposal retains a tally accumulator, a cursor, and a snapshot of the
bonded validators and tally parameters until all of its vote records have been
processed. Processed votes move to a round-specific archive so an application-state
export can reconstruct every vote while a tally is unfinished. New votes are rejected
after the accumulator is created. A vote's per-validator delegation snapshot is
created with the vote and refreshed by staking hooks whenever that voter delegates,
undelegates, or redelegates. The snapshot is frozen when tallying starts, at the same
point as the validator snapshot, and moves with the vote into the tally archive.
Delegator results are accumulated per validator from those stored shares, so changes
after tallying starts do not alter the result. If the stored delegation shares exceed
that validator's tally snapshot, every delegator option is scaled by the same factor
to fit the snapshotted voting-power budget. This makes the result independent of
vote-record order. Completed tally archives and their delegation snapshots are
removed incrementally under the same per-block vote-record budget, with part of that
budget reserved so cleanup cannot be starved by unfinished tallies.

The version 4 governance store migration records the first proposal ID that does not
need delegation-tracking backfill. When an older proposal reaches tallying, its
delegation snapshots and active-vote index entries are created incrementally with a
per-proposal cursor under the same per-block vote-record budget as tallying and
cleanup. New votes are rejected once that backfill starts, and tally accumulation
cannot start until it completes. Votes and proposals created after the upgrade
already have the required tracking data and skip backfill. Read-only tally and export
operations derive a missing snapshot while a proposal still needs backfill; after it
completes, tally processing treats a missing snapshot as an invariant failure.

Application-state export serializes all archived and pending votes together with their
delegation snapshots, but not the in-progress accumulator. On import, an expired voting
proposal starts a new tally from those votes, their frozen delegation snapshots, and
the imported validator and governance-parameter state. The accumulator is created
during genesis initialization, so the proposal does not reopen for votes before its
first `EndBlock`.
