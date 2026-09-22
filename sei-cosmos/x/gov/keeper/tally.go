package keeper

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

const cleanupCursorUnset byte = 0

type tallyProgress struct {
	Cursor     []byte
	BoundaryID []byte
	Expedited  bool
}

type tallyOptionResults struct {
	Yes        sdk.Dec
	Abstain    sdk.Dec
	No         sdk.Dec
	NoWithVeto sdk.Dec
}

type tallyElectorate struct {
	TotalBondedTokens sdk.Int
	TallyParams       types.TallyParams
	Validators        []tallyValidator
}

type tallyValidator struct {
	Address         string
	BondedTokens    sdk.Int
	DelegatorShares sdk.Dec
}

type tallyValidatorAccumulator struct {
	ObservedDelegatorShares sdk.Dec
	DelegatorResults        tallyOptionResults
	Vote                    types.WeightedVoteOptions
}

type tallyState struct {
	Electorate       tallyElectorate
	Validators       map[string]tallyValidator
	Accumulators     map[string]tallyValidatorAccumulator
	Changed          map[string]struct{}
	LoadAccumulators bool
}

// Tally calculates a proposal's result without changing its tally state.
func (keeper Keeper) Tally(ctx sdk.Context, proposal types.Proposal) (passes bool, burnDeposits bool, tallyResults types.TallyResult) {
	progress, found := tallyProgress{}, false
	if keeper.IncrementalTallyEnabled(ctx) {
		progress, found = keeper.getTallyProgress(ctx, proposal.ProposalId)
	}
	if !found {
		state := newTallyState(keeper.initializeTallyElectorate(ctx, proposal), false)
		store := ctx.KVStore(keeper.storeKey)
		votes := prefix.NewStore(store, types.VotesKey(proposal.ProposalId))
		keeper.iterateVoteStore(votes, func(vote types.Vote) bool {
			keeper.addVoteToTally(ctx, proposal.ProposalId, proposal.IsExpedited, &state, vote, keeper.voteDelegations(ctx, proposal.ProposalId, proposal.IsExpedited, vote))
			return false
		})
		return keeper.finishTally(ctx, proposal.ProposalId, proposal.IsExpedited, &state)
	}

	boundary := keeper.tallyProgressBoundary(ctx, proposal.ProposalId, progress)
	state := newTallyState(boundary.Electorate, true)
	store := ctx.KVStore(keeper.storeKey)
	votes := prefix.NewStore(store, types.VotesKey(proposal.ProposalId))
	keeper.iterateVoteStore(votes, func(vote types.Vote) bool {
		keeper.addVoteToTally(ctx, proposal.ProposalId, progress.Expedited, &state, vote, keeper.voteDelegations(ctx, proposal.ProposalId, progress.Expedited, vote))
		return false
	})
	return keeper.finishTally(ctx, proposal.ProposalId, progress.Expedited, &state)
}

// TallyLegacy calculates a proposal's result and removes its votes using the legacy tally transition.
func (keeper Keeper) TallyLegacy(ctx sdk.Context, proposal types.Proposal) (passes bool, burnDeposits bool, tallyResults types.TallyResult) {
	results := map[types.VoteOption]sdk.Dec{
		types.OptionYes:        sdk.ZeroDec(),
		types.OptionAbstain:    sdk.ZeroDec(),
		types.OptionNo:         sdk.ZeroDec(),
		types.OptionNoWithVeto: sdk.ZeroDec(),
	}
	totalVotingPower := sdk.ZeroDec()
	validators := make(map[string]types.ValidatorGovInfo)

	keeper.sk.IterateBondedValidatorsByPower(ctx, func(_ int64, validator stakingtypes.ValidatorI) bool {
		validators[validator.GetOperator().String()] = types.NewValidatorGovInfo(
			validator.GetOperator(),
			validator.GetBondedTokens(),
			validator.GetDelegatorShares(),
			sdk.ZeroDec(),
			types.WeightedVoteOptions{},
		)
		return false
	})

	keeper.IterateVotes(ctx, proposal.ProposalId, func(vote types.Vote) bool {
		voter := sdk.MustAccAddressFromBech32(vote.Voter)
		validatorAddress := sdk.ValAddress(voter.Bytes()).String()
		if validator, found := validators[validatorAddress]; found {
			validator.Vote = vote.Options
			validators[validatorAddress] = validator
		}

		keeper.sk.IterateDelegations(ctx, voter, func(_ int64, delegation stakingtypes.DelegationI) bool {
			validatorAddress := delegation.GetValidatorAddr().String()
			validator, found := validators[validatorAddress]
			if !found {
				return false
			}

			validator.DelegatorDeductions = validator.DelegatorDeductions.Add(delegation.GetShares())
			validators[validatorAddress] = validator
			votingPower := delegation.GetShares().MulInt(validator.BondedTokens).Quo(validator.DelegatorShares)
			for _, option := range vote.Options {
				results[option.Option] = results[option.Option].Add(votingPower.Mul(option.Weight))
			}
			totalVotingPower = totalVotingPower.Add(votingPower)
			return false
		})

		ctx.KVStore(keeper.storeKey).Delete(types.VoteKey(vote.ProposalId, voter))
		return false
	})

	for _, validator := range validators {
		if len(validator.Vote) == 0 {
			continue
		}

		sharesAfterDeductions := validator.DelegatorShares.Sub(validator.DelegatorDeductions)
		votingPower := sharesAfterDeductions.MulInt(validator.BondedTokens).Quo(validator.DelegatorShares)
		for _, option := range validator.Vote {
			results[option.Option] = results[option.Option].Add(votingPower.Mul(option.Weight))
		}
		totalVotingPower = totalVotingPower.Add(votingPower)
	}

	tallyParams := keeper.GetTallyParams(ctx)
	tallyResults = types.NewTallyResultFromMap(results)
	if keeper.sk.TotalBondedTokens(ctx).IsZero() {
		return false, false, tallyResults
	}

	percentVoting := totalVotingPower.Quo(keeper.sk.TotalBondedTokens(ctx).ToDec())
	if percentVoting.LT(tallyParams.GetQuorum(proposal.IsExpedited)) {
		return false, true, tallyResults
	}

	if totalVotingPower.Sub(results[types.OptionAbstain]).Equal(sdk.ZeroDec()) {
		return false, false, tallyResults
	}

	if results[types.OptionNoWithVeto].Quo(totalVotingPower).GT(tallyParams.VetoThreshold) {
		return false, true, tallyResults
	}

	if results[types.OptionYes].Quo(totalVotingPower.Sub(results[types.OptionAbstain])).GT(tallyParams.GetThreshold(proposal.IsExpedited)) {
		return true, false, tallyResults
	}

	return false, false, tallyResults
}

// TallyIncremental processes at most maxRecords governance work records after incremental tallying is active.
func (keeper Keeper) TallyIncremental(
	ctx sdk.Context,
	proposal types.Proposal,
	maxRecords int,
) (complete bool, processed int, passes bool, burnDeposits bool, tallyResults types.TallyResult) {
	if !keeper.IncrementalTallyEnabled(ctx) {
		passes, burnDeposits, tallyResults = keeper.TallyLegacy(ctx, proposal)
		return true, 0, passes, burnDeposits, tallyResults
	}
	if maxRecords < 0 {
		panic("maximum governance records to process cannot be negative")
	}
	progress, found := keeper.getTallyProgress(ctx, proposal.ProposalId)
	boundary, boundaryID, boundaryFound := keeper.getSelectedTallyBoundary(ctx, proposal.ProposalId)
	if !found && !boundaryFound && keeper.usesLegacyTallySemantics(ctx, proposal) {
		backfillComplete, backfilled := keeper.BackfillVoteDelegationTracking(ctx, proposal.ProposalId, maxRecords-processed)
		processed += backfilled
		if !backfillComplete {
			return false, processed, false, false, types.EmptyTallyResult()
		}
	}
	if !boundaryFound {
		if maxRecords == 0 {
			return false, processed, false, false, types.EmptyTallyResult()
		}
		boundary, boundaryID = keeper.selectTallyBoundary(ctx, proposal)
		if keeper.usesLegacyTallySemantics(ctx, proposal) {
			progress = newTallyProgress(boundaryID, proposal.IsExpedited)
			keeper.setTallyProgress(ctx, proposal.ProposalId, progress)
			return false, maxRecords, false, false, types.EmptyTallyResult()
		}
	}
	updatesComplete, updatesProcessed := keeper.ProcessVoteDelegationUpdatesThrough(
		ctx,
		maxRecords-processed,
		boundary.UpdateSequence,
	)
	processed += updatesProcessed
	if !updatesComplete {
		return false, processed, false, false, types.EmptyTallyResult()
	}

	if !found {
		progress = newTallyProgress(boundaryID, proposal.IsExpedited)
	} else if progress.Expedited != proposal.IsExpedited {
		panic(fmt.Sprintf("tally round for proposal %d changed", proposal.ProposalId))
	} else if !bytes.Equal(progress.BoundaryID, boundaryID) {
		panic(fmt.Sprintf("tally round for proposal %d changed boundary", proposal.ProposalId))
	}
	if processed == maxRecords {
		keeper.setTallyProgress(ctx, proposal.ProposalId, progress)
		return false, processed, false, false, types.EmptyTallyResult()
	}

	state := newTallyState(boundary.Electorate, found)
	var tallied int
	complete, tallied = keeper.processTallyVotes(ctx, proposal.ProposalId, &progress, &state, maxRecords-processed)
	processed += tallied
	if !complete {
		keeper.flushTallyValidatorAccumulators(ctx, proposal.ProposalId, progress.Expedited, &state)
		keeper.setTallyProgress(ctx, proposal.ProposalId, progress)
		return false, processed, false, false, types.EmptyTallyResult()
	}
	if processed == 0 {
		processed = 1
	}

	passes, burnDeposits, tallyResults = keeper.finishTally(ctx, proposal.ProposalId, progress.Expedited, &state)
	keeper.deleteTallyProgress(ctx, proposal.ProposalId)
	keeper.markTallyVotesForCleanup(ctx, proposal.ProposalId, progress.Expedited)
	keeper.markTallyValidatorAccumulatorsForCleanup(ctx, proposal.ProposalId, progress.Expedited)
	return true, processed, passes, burnDeposits, tallyResults
}

// IsTallying reports whether a proposal has an unfinished incremental tally.
func (keeper Keeper) IsTallying(ctx sdk.Context, proposalID uint64) bool {
	store := ctx.KVStore(keeper.storeKey)
	return store.Has(types.TallyProgressKey(proposalID))
}

// InitializeTally persists a proposal's tally accumulator when one does not exist.
func (keeper Keeper) InitializeTally(ctx sdk.Context, proposal types.Proposal) {
	if !keeper.IncrementalTallyEnabled(ctx) {
		return
	}
	progress, found := keeper.getTallyProgress(ctx, proposal.ProposalId)
	if found {
		if progress.Expedited != proposal.IsExpedited {
			panic(fmt.Sprintf("tally round for proposal %d changed", proposal.ProposalId))
		}
		return
	}
	if keeper.voteNeedsDelegationBackfill(ctx, proposal.ProposalId) {
		panic("cannot initialize tally while vote delegation backfill is in progress")
	}
	_, boundaryID := keeper.selectTallyBoundary(ctx, proposal)
	keeper.setTallyProgress(ctx, proposal.ProposalId, newTallyProgress(boundaryID, proposal.IsExpedited))
}

// CleanupTallyVotes deletes at most maxVotes records archived by completed tallies.
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

	if deleted == maxVotes {
		return deleted
	}
	return deleted + keeper.cleanupTallyValidatorAccumulators(ctx, maxVotes-deleted)
}

func (keeper Keeper) cleanupTallyValidatorAccumulators(ctx sdk.Context, maxRecords int) (deleted int) {
	store := ctx.KVStore(keeper.storeKey)
	iterator := sdk.KVStorePrefixIterator(store, types.TallyAccumulatorCleanupKeyPrefix)
	defer func() { _ = iterator.Close() }()

	for ; iterator.Valid() && deleted < maxRecords; iterator.Next() {
		proposalID, expedited := types.SplitTallyAccumulatorCleanupKey(iterator.Key())
		cursor := decodeCleanupCursor(iterator.Value())
		count, complete, nextCursor := keeper.cleanupProposalTallyValidatorAccumulators(
			ctx,
			proposalID,
			expedited,
			maxRecords-deleted,
			cursor,
		)
		deleted += count

		cleanupKey := types.TallyAccumulatorCleanupKey(proposalID, expedited)
		if complete {
			store.Delete(cleanupKey)
		} else {
			store.Set(cleanupKey, nextCursor)
		}
	}

	return deleted
}

func (keeper Keeper) initializeTallyElectorate(ctx sdk.Context, proposal types.Proposal) tallyElectorate {
	if keeper.IncrementalTallyEnabled(ctx) {
		if boundary, _, found := keeper.getSelectedTallyBoundary(ctx, proposal.ProposalId); found {
			return boundary.Electorate
		}
		if !keeper.usesLegacyTallySemantics(ctx, proposal) {
			if boundary, _, found := keeper.getDeadlineTallyBoundary(ctx, proposal.VotingEndTime); found {
				return boundary.Electorate
			}
		}
	}
	return keeper.snapshotTallyElectorate(ctx)
}

func newTallyProgress(boundaryID []byte, expedited bool) tallyProgress {
	return tallyProgress{
		BoundaryID: append([]byte(nil), boundaryID...),
		Expedited:  expedited,
	}
}

func (keeper Keeper) processTallyVotes(
	ctx sdk.Context,
	proposalID uint64,
	progress *tallyProgress,
	state *tallyState,
	maxVotes int,
) (complete bool, processed int) {
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
		if snapshotValue == nil {
			panic(fmt.Sprintf("missing delegation snapshot for proposal %d voter %s", proposalID, voter))
		}
		snapshot := keeper.unmarshalVoteDelegations(snapshotValue)
		keeper.addVoteToTally(ctx, proposalID, progress.Expedited, state, vote, snapshot)

		store.Set(types.TallyVoteKey(proposalID, progress.Expedited, voter), value)
		store.Set(types.TallyVoteDelegationsKey(proposalID, progress.Expedited, voter), snapshotValue)
		store.Delete(key)
		store.Delete(snapshotKey)
		store.Delete(types.VoterProposalsKey(voter, proposalID))
		keeper.deleteVoteDelegationSnapshotRevision(ctx, proposalID, voter)
		progress.Cursor = key
		processed++
	}

	return !iterator.Valid(), processed
}

func (keeper Keeper) addVoteToTally(
	ctx sdk.Context,
	proposalID uint64,
	expedited bool,
	state *tallyState,
	vote types.Vote,
	snapshot types.VoteDelegationSnapshot,
) {
	voter := sdk.MustAccAddressFromBech32(vote.Voter)
	if validator, ok := state.Validators[sdk.ValAddress(voter.Bytes()).String()]; ok {
		accumulator := keeper.tallyValidatorAccumulator(ctx, proposalID, expedited, state, validator)
		accumulator.Vote = vote.Options
		state.setTallyValidatorAccumulator(validator.Address, accumulator)
	}

	for _, delegation := range snapshot.Delegations {
		validator, ok := state.Validators[delegation.Validator]
		if !ok || validator.DelegatorShares.IsZero() {
			continue
		}

		accumulator := keeper.tallyValidatorAccumulator(ctx, proposalID, expedited, state, validator)
		votingShares := delegation.Shares
		accumulator.ObservedDelegatorShares = accumulator.ObservedDelegatorShares.Add(votingShares)
		votingPower := votingShares.MulInt(validator.BondedTokens).Quo(validator.DelegatorShares)
		accumulator.DelegatorResults.add(vote.Options, votingPower)
		state.setTallyValidatorAccumulator(validator.Address, accumulator)
	}
}

func (keeper Keeper) voteDelegations(
	ctx sdk.Context,
	proposalID uint64,
	expedited bool,
	vote types.Vote,
) types.VoteDelegationSnapshot {
	voter := sdk.MustAccAddressFromBech32(vote.Voter)
	if !keeper.IncrementalTallyEnabled(ctx) {
		return keeper.snapshotVoteDelegations(ctx, proposalID, voter)
	}
	store := ctx.KVStore(keeper.storeKey)
	if bz := store.Get(types.VoteDelegationsKey(proposalID, voter)); bz != nil {
		snapshot := keeper.unmarshalVoteDelegations(bz)
		return keeper.applyVoteDelegationSnapshotUpdates(ctx, proposalID, voter, snapshot)
	}
	if bz := store.Get(types.TallyVoteDelegationsKey(proposalID, expedited, voter)); bz != nil {
		return keeper.unmarshalVoteDelegations(bz)
	}
	if keeper.voteNeedsDelegationBackfill(ctx, proposalID) {
		return keeper.snapshotVoteDelegations(ctx, proposalID, voter)
	}
	panic(fmt.Sprintf("missing delegation snapshot for proposal %d voter %s", proposalID, voter))
}

func newTallyState(electorate tallyElectorate, loadAccumulators bool) tallyState {
	validators := make(map[string]tallyValidator, len(electorate.Validators))
	for _, validator := range electorate.Validators {
		validators[validator.Address] = validator
	}
	return tallyState{
		Electorate:       electorate,
		Validators:       validators,
		Accumulators:     make(map[string]tallyValidatorAccumulator),
		Changed:          make(map[string]struct{}),
		LoadAccumulators: loadAccumulators,
	}
}

func (keeper Keeper) tallyProgressBoundary(ctx sdk.Context, proposalID uint64, progress tallyProgress) tallyBoundary {
	if len(progress.BoundaryID) == 0 {
		panic(fmt.Sprintf("tally progress for proposal %d has no boundary", proposalID))
	}
	boundary, found := keeper.getTallyBoundary(ctx, progress.BoundaryID)
	if !found {
		panic(fmt.Sprintf("missing tally boundary for proposal %d", proposalID))
	}
	return boundary
}

func (keeper Keeper) tallyValidatorAccumulator(
	ctx sdk.Context,
	proposalID uint64,
	expedited bool,
	state *tallyState,
	validator tallyValidator,
) tallyValidatorAccumulator {
	if accumulator, found := state.Accumulators[validator.Address]; found {
		return accumulator
	}

	accumulator := newTallyValidatorAccumulator()
	if state.LoadAccumulators {
		validatorAddress, err := sdk.ValAddressFromBech32(validator.Address)
		if err != nil {
			panic(fmt.Errorf("invalid tally validator %q: %w", validator.Address, err))
		}
		bz := ctx.KVStore(keeper.storeKey).Get(
			types.TallyValidatorAccumulatorKey(proposalID, expedited, validatorAddress),
		)
		if bz != nil {
			var stored types.TallyValidatorAccumulator
			keeper.cdc.MustUnmarshal(bz, &stored)
			accumulator = tallyValidatorAccumulatorFromProto(stored)
		}
	}
	state.Accumulators[validator.Address] = accumulator
	return accumulator
}

func (state *tallyState) setTallyValidatorAccumulator(address string, accumulator tallyValidatorAccumulator) {
	state.Accumulators[address] = accumulator
	state.Changed[address] = struct{}{}
}

func (keeper Keeper) flushTallyValidatorAccumulators(
	ctx sdk.Context,
	proposalID uint64,
	expedited bool,
	state *tallyState,
) {
	addresses := make([]string, 0, len(state.Changed))
	for address := range state.Changed {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)

	store := ctx.KVStore(keeper.storeKey)
	for _, address := range addresses {
		validator, found := state.Validators[address]
		if !found {
			panic(fmt.Sprintf("missing tally validator %q", address))
		}
		validatorAddress, err := sdk.ValAddressFromBech32(validator.Address)
		if err != nil {
			panic(fmt.Errorf("invalid tally validator %q: %w", validator.Address, err))
		}
		accumulator := state.Accumulators[address]
		encoded := tallyValidatorAccumulatorToProto(accumulator)
		store.Set(
			types.TallyValidatorAccumulatorKey(proposalID, expedited, validatorAddress),
			keeper.cdc.MustMarshal(&encoded),
		)
	}
}

func (keeper Keeper) markTallyValidatorAccumulatorsForCleanup(ctx sdk.Context, proposalID uint64, expedited bool) {
	store := ctx.KVStore(keeper.storeKey)
	iterator := sdk.KVStorePrefixIterator(store, types.TallyValidatorAccumulatorsKey(proposalID, expedited))
	defer func() { _ = iterator.Close() }()

	if iterator.Valid() {
		store.Set(types.TallyAccumulatorCleanupKey(proposalID, expedited), []byte{cleanupCursorUnset})
	}
}

func (keeper Keeper) finishTally(
	ctx sdk.Context,
	proposalID uint64,
	expedited bool,
	state *tallyState,
) (passes bool, burnDeposits bool, tallyResults types.TallyResult) {
	results := newTallyOptionResults()
	totalVotingPower := sdk.ZeroDec()
	for _, validator := range state.Electorate.Validators {
		accumulator := keeper.tallyValidatorAccumulator(ctx, proposalID, expedited, state, validator)
		totalVotingPower = addValidatorResults(&results, totalVotingPower, validator, accumulator)
	}

	tallyResults = results.tallyResult()
	if state.Electorate.TotalBondedTokens.IsZero() {
		return false, false, tallyResults
	}

	percentVoting := totalVotingPower.Quo(state.Electorate.TotalBondedTokens.ToDec())
	if percentVoting.LT(state.Electorate.TallyParams.GetQuorum(expedited)) {
		return false, true, tallyResults
	}

	if totalVotingPower.Sub(results.Abstain).IsZero() {
		return false, false, tallyResults
	}

	if results.NoWithVeto.Quo(totalVotingPower).GT(state.Electorate.TallyParams.VetoThreshold) {
		return false, true, tallyResults
	}

	nonAbstainingPower := totalVotingPower.Sub(results.Abstain)
	if results.Yes.Quo(nonAbstainingPower).GT(state.Electorate.TallyParams.GetThreshold(expedited)) {
		return true, false, tallyResults
	}

	return false, false, tallyResults
}

func addValidatorResults(
	results *tallyOptionResults,
	totalVotingPower sdk.Dec,
	validator tallyValidator,
	accumulator tallyValidatorAccumulator,
) sdk.Dec {
	if validator.DelegatorShares.IsZero() {
		return totalVotingPower
	}

	countedDelegatorShares := accumulator.ObservedDelegatorShares
	delegatorScale := sdk.OneDec()
	if countedDelegatorShares.GT(validator.DelegatorShares) {
		delegatorScale = validator.DelegatorShares.Quo(countedDelegatorShares)
		countedDelegatorShares = validator.DelegatorShares
	}

	results.addScaled(accumulator.DelegatorResults, delegatorScale)
	delegatorVotingPower := countedDelegatorShares.MulInt(validator.BondedTokens).Quo(validator.DelegatorShares)
	totalVotingPower = totalVotingPower.Add(delegatorVotingPower)

	if len(accumulator.Vote) == 0 {
		return totalVotingPower
	}
	validatorShares := validator.DelegatorShares.Sub(countedDelegatorShares)
	validatorVotingPower := validatorShares.MulInt(validator.BondedTokens).Quo(validator.DelegatorShares)
	results.add(accumulator.Vote, validatorVotingPower)
	return totalVotingPower.Add(validatorVotingPower)
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

func newTallyValidatorAccumulator() tallyValidatorAccumulator {
	return tallyValidatorAccumulator{
		ObservedDelegatorShares: sdk.ZeroDec(),
		DelegatorResults:        newTallyOptionResults(),
		Vote:                    types.WeightedVoteOptions{},
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

func tallyOptionResultsToProto(results tallyOptionResults) types.TallyOptionResults {
	return types.TallyOptionResults{
		Yes:        results.Yes,
		Abstain:    results.Abstain,
		No:         results.No,
		NoWithVeto: results.NoWithVeto,
	}
}

func tallyOptionResultsFromProto(results types.TallyOptionResults) tallyOptionResults {
	return tallyOptionResults{
		Yes:        results.Yes,
		Abstain:    results.Abstain,
		No:         results.No,
		NoWithVeto: results.NoWithVeto,
	}
}

func tallyValidatorAccumulatorToProto(accumulator tallyValidatorAccumulator) types.TallyValidatorAccumulator {
	return types.TallyValidatorAccumulator{
		ObservedDelegatorShares: accumulator.ObservedDelegatorShares,
		DelegatorResults:        tallyOptionResultsToProto(accumulator.DelegatorResults),
		Vote:                    accumulator.Vote,
	}
}

func tallyValidatorAccumulatorFromProto(accumulator types.TallyValidatorAccumulator) tallyValidatorAccumulator {
	return tallyValidatorAccumulator{
		ObservedDelegatorShares: accumulator.ObservedDelegatorShares,
		DelegatorResults:        tallyOptionResultsFromProto(accumulator.DelegatorResults),
		Vote:                    accumulator.Vote,
	}
}

func tallyProgressToProto(progress tallyProgress) types.TallyProgress {
	return types.TallyProgress{
		Cursor:     progress.Cursor,
		BoundaryId: progress.BoundaryID,
		Expedited:  progress.Expedited,
	}
}

func tallyProgressFromProto(progress types.TallyProgress) tallyProgress {
	return tallyProgress{
		Cursor:     append([]byte(nil), progress.Cursor...),
		BoundaryID: append([]byte(nil), progress.BoundaryId...),
		Expedited:  progress.Expedited,
	}
}

func (keeper Keeper) getTallyProgress(ctx sdk.Context, proposalID uint64) (progress tallyProgress, found bool) {
	store := ctx.KVStore(keeper.storeKey)
	bz := store.Get(types.TallyProgressKey(proposalID))
	if bz == nil {
		return tallyProgress{}, false
	}
	var stored types.TallyProgress
	keeper.cdc.MustUnmarshal(bz, &stored)
	return tallyProgressFromProto(stored), true
}

func (keeper Keeper) setTallyProgress(ctx sdk.Context, proposalID uint64, progress tallyProgress) {
	stored := tallyProgressToProto(progress)
	ctx.KVStore(keeper.storeKey).Set(types.TallyProgressKey(proposalID), keeper.cdc.MustMarshal(&stored))
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
	return deleted, complete, cursor
}

func (keeper Keeper) cleanupProposalTallyValidatorAccumulators(
	ctx sdk.Context,
	proposalID uint64,
	expedited bool,
	maxRecords int,
	after []byte,
) (deleted int, complete bool, cursor []byte) {
	store := ctx.KVStore(keeper.storeKey)
	accumulatorsPrefix := types.TallyValidatorAccumulatorsKey(proposalID, expedited)
	start := accumulatorsPrefix
	if len(after) != 0 {
		start = sdk.PrefixEndBytes(after)
	}
	iterator := store.Iterator(start, sdk.PrefixEndBytes(accumulatorsPrefix))
	defer func() { _ = iterator.Close() }()

	for ; iterator.Valid() && deleted < maxRecords; iterator.Next() {
		cursor = append(cursor[:0], iterator.Key()...)
		store.Delete(iterator.Key())
		deleted++
	}

	return deleted, !iterator.Valid(), cursor
}

func decodeCleanupCursor(value []byte) []byte {
	if len(value) == 1 && value[0] == cleanupCursorUnset {
		return nil
	}
	return append([]byte(nil), value...)
}
