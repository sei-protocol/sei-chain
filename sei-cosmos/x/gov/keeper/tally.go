package keeper

import (
	"encoding/json"
	"fmt"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

const cleanupCursorUnset byte = 0

type tallyProgress struct {
	Cursor            []byte             `json:"cursor,omitempty"`
	Results           tallyOptionResults `json:"results"`
	TotalVotingPower  sdk.Dec            `json:"total_voting_power"`
	TotalBondedTokens sdk.Int            `json:"total_bonded_tokens"`
	TallyParams       types.TallyParams  `json:"tally_params"`
	Validators        []tallyValidator   `json:"validators"`
	Expedited         bool               `json:"expedited"`
}

type tallyOptionResults struct {
	Yes        sdk.Dec `json:"yes"`
	Abstain    sdk.Dec `json:"abstain"`
	No         sdk.Dec `json:"no"`
	NoWithVeto sdk.Dec `json:"no_with_veto"`
}

type tallyValidator struct {
	Address                 string                    `json:"address"`
	BondedTokens            sdk.Int                   `json:"bonded_tokens"`
	DelegatorShares         sdk.Dec                   `json:"delegator_shares"`
	ObservedDelegatorShares sdk.Dec                   `json:"observed_delegator_shares"`
	DelegatorResults        tallyOptionResults        `json:"delegator_results"`
	Vote                    types.WeightedVoteOptions `json:"vote"`
}

// Tally calculates a proposal's result without changing its tally state.
func (keeper Keeper) Tally(ctx sdk.Context, proposal types.Proposal) (passes bool, burnDeposits bool, tallyResults types.TallyResult) {
	progress := keeper.initializeTally(ctx, proposal)
	validators := progress.validatorMap()
	keeper.IterateVotes(ctx, proposal.ProposalId, func(vote types.Vote) bool {
		keeper.addVoteToTally(validators, vote, keeper.voteDelegations(ctx, proposal.ProposalId, progress.Expedited, vote))
		return false
	})
	return keeper.finishTally(progress)
}

// TallyIncremental processes at most maxVotes vote records and persists an unfinished tally.
func (keeper Keeper) TallyIncremental(
	ctx sdk.Context,
	proposal types.Proposal,
	maxVotes int,
) (complete bool, processed int, passes bool, burnDeposits bool, tallyResults types.TallyResult) {
	if maxVotes < 0 {
		panic("maximum votes to tally cannot be negative")
	}

	progress, found := keeper.getTallyProgress(ctx, proposal.ProposalId)
	if !found {
		progress = keeper.initializeTally(ctx, proposal)
	} else if progress.Expedited != proposal.IsExpedited {
		panic(fmt.Sprintf("tally round for proposal %d changed", proposal.ProposalId))
	}

	complete, processed = keeper.processTallyVotes(ctx, proposal.ProposalId, &progress, maxVotes)
	if !complete {
		keeper.setTallyProgress(ctx, proposal.ProposalId, progress)
		return false, processed, false, false, types.EmptyTallyResult()
	}

	passes, burnDeposits, tallyResults = keeper.finishTally(progress)
	keeper.deleteTallyProgress(ctx, proposal.ProposalId)
	keeper.markTallyVotesForCleanup(ctx, proposal.ProposalId, progress.Expedited)
	return true, processed, passes, burnDeposits, tallyResults
}

// IsTallying reports whether a proposal has an unfinished incremental tally.
func (keeper Keeper) IsTallying(ctx sdk.Context, proposalID uint64) bool {
	store := ctx.KVStore(keeper.storeKey)
	return store.Has(types.TallyProgressKey(proposalID))
}

// InitializeTally persists a proposal's tally accumulator when one does not exist.
func (keeper Keeper) InitializeTally(ctx sdk.Context, proposal types.Proposal) {
	progress, found := keeper.getTallyProgress(ctx, proposal.ProposalId)
	if found {
		if progress.Expedited != proposal.IsExpedited {
			panic(fmt.Sprintf("tally round for proposal %d changed", proposal.ProposalId))
		}
		return
	}
	keeper.setTallyProgress(ctx, proposal.ProposalId, keeper.initializeTally(ctx, proposal))
}

// CleanupTallyVotes deletes at most maxVotes vote records archived by completed tallies.
func (keeper Keeper) CleanupTallyVotes(ctx sdk.Context, maxVotes int) (deleted int) {
	if maxVotes <= 0 {
		return 0
	}

	store := ctx.KVStore(keeper.storeKey)
	iterator := sdk.KVStorePrefixIterator(store, types.TallyCleanupKeyPrefix)
	defer func() { _ = iterator.Close() }()

	for ; iterator.Valid() && deleted < maxVotes; iterator.Next() {
		proposalID, expedited := types.SplitTallyCleanupKey(iterator.Key())
		cursor := decodeCleanupCursor(iterator.Value())
		count, complete, nextCursor := keeper.cleanupProposalTallyVotes(
			ctx,
			proposalID,
			expedited,
			maxVotes-deleted,
			cursor,
		)
		deleted += count

		cleanupKey := types.TallyCleanupKey(proposalID, expedited)
		if complete {
			store.Delete(cleanupKey)
		} else {
			store.Set(cleanupKey, nextCursor)
		}
	}

	return deleted
}

func (keeper Keeper) initializeTally(ctx sdk.Context, proposal types.Proposal) tallyProgress {
	progress := tallyProgress{
		Results:           newTallyOptionResults(),
		TotalVotingPower:  sdk.ZeroDec(),
		TotalBondedTokens: keeper.sk.TotalBondedTokens(ctx),
		TallyParams:       keeper.GetTallyParams(ctx),
		Expedited:         proposal.IsExpedited,
	}

	keeper.sk.IterateBondedValidatorsByPower(ctx, func(_ int64, validator stakingtypes.ValidatorI) bool {
		progress.Validators = append(progress.Validators, tallyValidator{
			Address:                 validator.GetOperator().String(),
			BondedTokens:            validator.GetBondedTokens(),
			DelegatorShares:         validator.GetDelegatorShares(),
			ObservedDelegatorShares: sdk.ZeroDec(),
			DelegatorResults:        newTallyOptionResults(),
		})
		return false
	})

	return progress
}

func (keeper Keeper) processTallyVotes(
	ctx sdk.Context,
	proposalID uint64,
	progress *tallyProgress,
	maxVotes int,
) (complete bool, processed int) {
	validators := progress.validatorMap()

	store := ctx.KVStore(keeper.storeKey)
	votesPrefix := types.VotesKey(proposalID)
	start := votesPrefix
	if len(progress.Cursor) != 0 {
		start = sdk.PrefixEndBytes(progress.Cursor)
	}
	iterator := store.Iterator(start, sdk.PrefixEndBytes(votesPrefix))
	defer func() { _ = iterator.Close() }()

	for ; iterator.Valid() && processed < maxVotes; iterator.Next() {
		key := append([]byte(nil), iterator.Key()...)
		value := append([]byte(nil), iterator.Value()...)

		var vote types.Vote
		keeper.cdc.MustUnmarshal(value, &vote)
		populateLegacyOption(&vote)

		voter := sdk.MustAccAddressFromBech32(vote.Voter)
		snapshotKey := types.VoteDelegationsKey(proposalID, voter)
		snapshotValue := store.Get(snapshotKey)
		var snapshot types.VoteDelegationSnapshot
		if snapshotValue == nil {
			snapshot = keeper.snapshotVoteDelegations(ctx, proposalID, voter)
			snapshotValue = keeper.cdc.MustMarshal(&snapshot)
		} else {
			snapshot = keeper.unmarshalVoteDelegations(snapshotValue)
		}
		keeper.addVoteToTally(validators, vote, snapshot)

		store.Set(types.TallyVoteKey(proposalID, progress.Expedited, voter), value)
		store.Set(types.TallyVoteDelegationsKey(proposalID, progress.Expedited, voter), snapshotValue)
		store.Delete(key)
		store.Delete(snapshotKey)
		store.Delete(types.VoterProposalsKey(voter, proposalID))
		progress.Cursor = key
		processed++
	}

	return !iterator.Valid(), processed
}

func (keeper Keeper) addVoteToTally(
	validators map[string]*tallyValidator,
	vote types.Vote,
	snapshot types.VoteDelegationSnapshot,
) {
	voter := sdk.MustAccAddressFromBech32(vote.Voter)
	if validator, ok := validators[sdk.ValAddress(voter.Bytes()).String()]; ok {
		validator.Vote = vote.Options
	}

	for _, delegation := range snapshot.Delegations {
		validator, ok := validators[delegation.Validator]
		if !ok || validator.DelegatorShares.IsZero() {
			continue
		}

		votingShares := delegation.Shares
		validator.ObservedDelegatorShares = validator.ObservedDelegatorShares.Add(votingShares)
		votingPower := votingShares.MulInt(validator.BondedTokens).Quo(validator.DelegatorShares)
		validator.DelegatorResults.add(vote.Options, votingPower)
	}
}

func (keeper Keeper) voteDelegations(
	ctx sdk.Context,
	proposalID uint64,
	expedited bool,
	vote types.Vote,
) types.VoteDelegationSnapshot {
	voter := sdk.MustAccAddressFromBech32(vote.Voter)
	store := ctx.KVStore(keeper.storeKey)
	if bz := store.Get(types.VoteDelegationsKey(proposalID, voter)); bz != nil {
		return keeper.unmarshalVoteDelegations(bz)
	}
	if bz := store.Get(types.TallyVoteDelegationsKey(proposalID, expedited, voter)); bz != nil {
		return keeper.unmarshalVoteDelegations(bz)
	}
	return keeper.snapshotVoteDelegations(ctx, proposalID, voter)
}

func (progress *tallyProgress) validatorMap() map[string]*tallyValidator {
	validators := make(map[string]*tallyValidator, len(progress.Validators))
	for i := range progress.Validators {
		validator := &progress.Validators[i]
		validators[validator.Address] = validator
	}
	return validators
}

func (keeper Keeper) finishTally(progress tallyProgress) (passes bool, burnDeposits bool, tallyResults types.TallyResult) {
	for _, validator := range progress.Validators {
		progress.addValidatorResults(validator)
	}

	tallyResults = progress.Results.tallyResult()
	if progress.TotalBondedTokens.IsZero() {
		return false, false, tallyResults
	}

	percentVoting := progress.TotalVotingPower.Quo(progress.TotalBondedTokens.ToDec())
	if percentVoting.LT(progress.TallyParams.GetQuorum(progress.Expedited)) {
		return false, true, tallyResults
	}

	if progress.TotalVotingPower.Sub(progress.Results.Abstain).IsZero() {
		return false, false, tallyResults
	}

	if progress.Results.NoWithVeto.Quo(progress.TotalVotingPower).GT(progress.TallyParams.VetoThreshold) {
		return false, true, tallyResults
	}

	nonAbstainingPower := progress.TotalVotingPower.Sub(progress.Results.Abstain)
	if progress.Results.Yes.Quo(nonAbstainingPower).GT(progress.TallyParams.GetThreshold(progress.Expedited)) {
		return true, false, tallyResults
	}

	return false, false, tallyResults
}

func (progress *tallyProgress) addValidatorResults(validator tallyValidator) {
	if validator.DelegatorShares.IsZero() {
		return
	}

	countedDelegatorShares := validator.ObservedDelegatorShares
	delegatorScale := sdk.OneDec()
	if countedDelegatorShares.GT(validator.DelegatorShares) {
		delegatorScale = validator.DelegatorShares.Quo(countedDelegatorShares)
		countedDelegatorShares = validator.DelegatorShares
	}

	progress.Results.addScaled(validator.DelegatorResults, delegatorScale)
	delegatorVotingPower := countedDelegatorShares.MulInt(validator.BondedTokens).Quo(validator.DelegatorShares)
	progress.TotalVotingPower = progress.TotalVotingPower.Add(delegatorVotingPower)

	if len(validator.Vote) == 0 {
		return
	}
	validatorShares := validator.DelegatorShares.Sub(countedDelegatorShares)
	validatorVotingPower := validatorShares.MulInt(validator.BondedTokens).Quo(validator.DelegatorShares)
	progress.Results.add(validator.Vote, validatorVotingPower)
	progress.TotalVotingPower = progress.TotalVotingPower.Add(validatorVotingPower)
}

func (results *tallyOptionResults) add(options types.WeightedVoteOptions, votingPower sdk.Dec) {
	for _, option := range options {
		subPower := votingPower.Mul(option.Weight)
		switch option.Option {
		case types.OptionYes:
			results.Yes = results.Yes.Add(subPower)
		case types.OptionAbstain:
			results.Abstain = results.Abstain.Add(subPower)
		case types.OptionNo:
			results.No = results.No.Add(subPower)
		case types.OptionNoWithVeto:
			results.NoWithVeto = results.NoWithVeto.Add(subPower)
		default:
			panic(fmt.Sprintf("unsupported vote option %s", option.Option))
		}
	}
}

func (results *tallyOptionResults) addScaled(other tallyOptionResults, scale sdk.Dec) {
	results.Yes = results.Yes.Add(other.Yes.Mul(scale))
	results.Abstain = results.Abstain.Add(other.Abstain.Mul(scale))
	results.No = results.No.Add(other.No.Mul(scale))
	results.NoWithVeto = results.NoWithVeto.Add(other.NoWithVeto.Mul(scale))
}

func newTallyOptionResults() tallyOptionResults {
	return tallyOptionResults{
		Yes:        sdk.ZeroDec(),
		Abstain:    sdk.ZeroDec(),
		No:         sdk.ZeroDec(),
		NoWithVeto: sdk.ZeroDec(),
	}
}

func (results tallyOptionResults) tallyResult() types.TallyResult {
	return types.NewTallyResult(
		results.Yes.TruncateInt(),
		results.Abstain.TruncateInt(),
		results.No.TruncateInt(),
		results.NoWithVeto.TruncateInt(),
	)
}

func (keeper Keeper) getTallyProgress(ctx sdk.Context, proposalID uint64) (progress tallyProgress, found bool) {
	store := ctx.KVStore(keeper.storeKey)
	bz := store.Get(types.TallyProgressKey(proposalID))
	if bz == nil {
		return tallyProgress{}, false
	}
	if err := json.Unmarshal(bz, &progress); err != nil {
		panic(fmt.Errorf("unmarshal tally progress for proposal %d: %w", proposalID, err))
	}
	return progress, true
}

func (keeper Keeper) setTallyProgress(ctx sdk.Context, proposalID uint64, progress tallyProgress) {
	bz, err := json.Marshal(progress)
	if err != nil {
		panic(fmt.Errorf("marshal tally progress for proposal %d: %w", proposalID, err))
	}
	ctx.KVStore(keeper.storeKey).Set(types.TallyProgressKey(proposalID), bz)
}

func (keeper Keeper) deleteTallyProgress(ctx sdk.Context, proposalID uint64) {
	ctx.KVStore(keeper.storeKey).Delete(types.TallyProgressKey(proposalID))
}

func (keeper Keeper) markTallyVotesForCleanup(ctx sdk.Context, proposalID uint64, expedited bool) {
	store := ctx.KVStore(keeper.storeKey)
	iterator := sdk.KVStorePrefixIterator(store, types.TallyVotesKey(proposalID, expedited))
	defer func() { _ = iterator.Close() }()
	if iterator.Valid() {
		store.Set(types.TallyCleanupKey(proposalID, expedited), []byte{cleanupCursorUnset})
	}
}

func (keeper Keeper) cleanupProposalTallyVotes(
	ctx sdk.Context,
	proposalID uint64,
	expedited bool,
	maxVotes int,
	after []byte,
) (deleted int, complete bool, cursor []byte) {
	store := ctx.KVStore(keeper.storeKey)
	votesPrefix := types.TallyVotesKey(proposalID, expedited)
	start := votesPrefix
	if len(after) != 0 {
		start = sdk.PrefixEndBytes(after)
	}
	iterator := store.Iterator(start, sdk.PrefixEndBytes(votesPrefix))
	defer func() { _ = iterator.Close() }()

	for ; iterator.Valid() && deleted < maxVotes; iterator.Next() {
		cursor = append(cursor[:0], iterator.Key()...)
		store.Delete(iterator.Key())
		snapshotKey := types.TallyVoteDelegationsKeyFromVoteKey(iterator.Key())
		store.Delete(snapshotKey)
		deleted++
	}

	complete = !iterator.Valid()
	if complete {
		store.Delete(types.TallyCleanupKey(proposalID, expedited))
	}
	return deleted, complete, cursor
}

func decodeCleanupCursor(value []byte) []byte {
	if len(value) == 1 && value[0] == cleanupCursorUnset {
		return nil
	}
	return append([]byte(nil), value...)
}
