package keeper

import (
	"encoding/json"
	"fmt"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

const (
	gapTallyBoundary      byte = 'g'
	exactTallyBoundary    byte = 'e'
	proposalTallyBoundary byte = 'p'
)

type tallyElectorate struct {
	TotalBondedTokens sdk.Int           `json:"total_bonded_tokens"`
	TallyParams       types.TallyParams `json:"tally_params"`
	Validators        []tallyValidator  `json:"validators"`
}

type tallyBoundary struct {
	LowerTime      time.Time       `json:"lower_time"`
	UpperTime      time.Time       `json:"upper_time"`
	UpdateSequence uint64          `json:"update_sequence"`
	Electorate     tallyElectorate `json:"electorate"`
}

// CaptureGapTallyBoundary freezes one electorate for proposal deadlines between consecutive block times.
func (keeper Keeper) CaptureGapTallyBoundary(ctx sdk.Context) {
	if !keeper.IncrementalTallyEnabled(ctx) {
		return
	}

	store := ctx.KVStore(keeper.storeKey)
	previousValue := store.Get(types.DeadlineBoundaryBlockTimeKey)
	if previousValue == nil {
		return
	}
	previous := parseBoundaryTime(previousValue)
	current := ctx.BlockTime()
	if !previous.Before(current) {
		return
	}

	if keeper.hasProposalDeadlineBetween(ctx, previous, current) {
		boundaryID := gapTallyBoundaryID(current)
		keeper.setTallyBoundary(ctx, boundaryID, tallyBoundary{
			LowerTime:      previous,
			UpperTime:      current,
			UpdateSequence: keeper.voteDelegationUpdateSequence(ctx),
			Electorate:     keeper.snapshotTallyElectorate(ctx),
		})
		store.Set(types.GapTallyBoundaryKey(current), boundaryID)
	}
	store.Set(types.DeadlineBoundaryBlockTimeKey, sdk.FormatTimeBytes(current))
}

// CaptureExactTallyBoundary freezes one electorate for proposal deadlines equal to the current block time.
func (keeper Keeper) CaptureExactTallyBoundary(ctx sdk.Context) {
	if !keeper.IncrementalTallyEnabled(ctx) || !keeper.hasProposalDeadlineAt(ctx, ctx.BlockTime()) {
		return
	}

	store := ctx.KVStore(keeper.storeKey)
	indexKey := types.ExactTallyBoundaryKey(ctx.BlockTime())
	if store.Has(indexKey) {
		return
	}
	boundaryID := exactTallyBoundaryID(ctx.BlockTime())
	keeper.setTallyBoundary(ctx, boundaryID, tallyBoundary{
		LowerTime:      ctx.BlockTime(),
		UpperTime:      ctx.BlockTime(),
		UpdateSequence: keeper.voteDelegationUpdateSequence(ctx),
		Electorate:     keeper.snapshotTallyElectorate(ctx),
	})
	store.Set(indexKey, boundaryID)
}

// InitializeDeadlineBoundaryClock records the block time preceding future deadline captures.
func (keeper Keeper) InitializeDeadlineBoundaryClock(ctx sdk.Context) {
	if !keeper.IncrementalTallyEnabled(ctx) {
		return
	}
	ctx.KVStore(keeper.storeKey).Set(types.DeadlineBoundaryBlockTimeKey, sdk.FormatTimeBytes(ctx.BlockTime()))
}

func (keeper Keeper) snapshotTallyElectorate(ctx sdk.Context) tallyElectorate {
	electorate := tallyElectorate{
		TotalBondedTokens: keeper.sk.TotalBondedTokens(ctx),
		TallyParams:       keeper.GetTallyParams(ctx),
		Validators:        []tallyValidator{},
	}
	keeper.sk.IterateBondedValidatorsByPower(ctx, func(_ int64, validator stakingtypes.ValidatorI) bool {
		electorate.Validators = append(electorate.Validators, tallyValidator{
			Address:                 validator.GetOperator().String(),
			BondedTokens:            validator.GetBondedTokens(),
			DelegatorShares:         validator.GetDelegatorShares(),
			ObservedDelegatorShares: sdk.ZeroDec(),
			DelegatorResults:        newTallyOptionResults(),
		})
		return false
	})
	return electorate
}

func (keeper Keeper) selectTallyBoundary(ctx sdk.Context, proposal types.Proposal) (tallyBoundary, []byte) {
	if boundary, boundaryID, found := keeper.getSelectedTallyBoundary(ctx, proposal.ProposalId); found {
		return boundary, boundaryID
	}

	if !keeper.usesLegacyTallySemantics(ctx, proposal) {
		if boundary, boundaryID, found := keeper.getDeadlineTallyBoundary(ctx, proposal.VotingEndTime); found {
			keeper.setProposalTallyBoundary(ctx, proposal.ProposalId, boundaryID)
			return boundary, boundaryID
		}
	}

	boundaryID := proposalSpecificTallyBoundaryID(proposal.ProposalId)
	boundary := tallyBoundary{
		LowerTime:      ctx.BlockTime(),
		UpperTime:      ctx.BlockTime(),
		UpdateSequence: keeper.voteDelegationUpdateSequence(ctx),
		Electorate:     keeper.snapshotTallyElectorate(ctx),
	}
	keeper.setTallyBoundary(ctx, boundaryID, boundary)
	keeper.setProposalTallyBoundary(ctx, proposal.ProposalId, boundaryID)
	return boundary, boundaryID
}

// ExportTallyElectorate returns the frozen electorate needed to restart a proposal tally.
func (keeper Keeper) ExportTallyElectorate(
	ctx sdk.Context,
	proposal types.Proposal,
) (types.TallyElectorate, bool) {
	if progress, found := keeper.getTallyProgress(ctx, proposal.ProposalId); found {
		return tallyElectorateToGenesis(proposal.ProposalId, tallyElectorate{
			TotalBondedTokens: progress.TotalBondedTokens,
			TallyParams:       progress.TallyParams,
			Validators:        progress.Validators,
		}), true
	}
	if boundary, _, found := keeper.getSelectedTallyBoundary(ctx, proposal.ProposalId); found {
		return tallyElectorateToGenesis(proposal.ProposalId, boundary.Electorate), true
	}
	if !keeper.usesLegacyTallySemantics(ctx, proposal) {
		if boundary, _, found := keeper.getDeadlineTallyBoundary(ctx, proposal.VotingEndTime); found {
			return tallyElectorateToGenesis(proposal.ProposalId, boundary.Electorate), true
		}
	}
	if proposal.Status == types.StatusVotingPeriod && !proposal.VotingEndTime.After(ctx.BlockTime()) {
		return tallyElectorateToGenesis(proposal.ProposalId, keeper.snapshotTallyElectorate(ctx)), true
	}
	return types.TallyElectorate{}, false
}

// SetTallyElectorate stores an imported frozen electorate for a proposal tally.
func (keeper Keeper) SetTallyElectorate(ctx sdk.Context, electorate types.TallyElectorate) {
	boundaryID := proposalSpecificTallyBoundaryID(electorate.ProposalId)
	keeper.setTallyBoundary(ctx, boundaryID, tallyBoundary{
		LowerTime:      ctx.BlockTime(),
		UpperTime:      ctx.BlockTime(),
		UpdateSequence: keeper.voteDelegationUpdateSequence(ctx),
		Electorate:     tallyElectorateFromGenesis(electorate),
	})
	keeper.setProposalTallyBoundary(ctx, electorate.ProposalId, boundaryID)
}

func (keeper Keeper) getSelectedTallyBoundary(
	ctx sdk.Context,
	proposalID uint64,
) (tallyBoundary, []byte, bool) {
	store := ctx.KVStore(keeper.storeKey)
	boundaryID := store.Get(types.ProposalTallyBoundaryKey(proposalID))
	if boundaryID == nil {
		return tallyBoundary{}, nil, false
	}
	boundary, found := keeper.getTallyBoundary(ctx, boundaryID)
	if !found {
		panic(fmt.Sprintf("missing tally boundary for proposal %d", proposalID))
	}
	return boundary, boundaryID, true
}

func (keeper Keeper) getDeadlineTallyBoundary(
	ctx sdk.Context,
	endTime time.Time,
) (tallyBoundary, []byte, bool) {
	store := ctx.KVStore(keeper.storeKey)
	if boundaryID := store.Get(types.ExactTallyBoundaryKey(endTime)); boundaryID != nil {
		boundary, found := keeper.getTallyBoundary(ctx, boundaryID)
		if !found {
			panic(fmt.Sprintf("missing exact tally boundary at %s", endTime))
		}
		return boundary, boundaryID, true
	}

	start := sdk.PrefixEndBytes(types.GapTallyBoundaryKey(endTime))
	iterator := store.Iterator(start, sdk.PrefixEndBytes(types.GapTallyBoundaryKeyPrefix))
	defer func() { _ = iterator.Close() }()
	if !iterator.Valid() {
		return tallyBoundary{}, nil, false
	}
	boundaryID := append([]byte(nil), iterator.Value()...)
	boundary, found := keeper.getTallyBoundary(ctx, boundaryID)
	if !found {
		panic(fmt.Sprintf("missing gap tally boundary for deadline %s", endTime))
	}
	if !boundary.LowerTime.Before(endTime) || !endTime.Before(boundary.UpperTime) {
		return tallyBoundary{}, nil, false
	}
	return boundary, boundaryID, true
}

func (keeper Keeper) proposalTallyBoundarySequence(ctx sdk.Context, proposal types.Proposal) (uint64, bool) {
	if boundary, _, found := keeper.getSelectedTallyBoundary(ctx, proposal.ProposalId); found {
		return boundary.UpdateSequence, true
	}
	if keeper.usesLegacyTallySemantics(ctx, proposal) {
		return 0, false
	}
	boundary, _, found := keeper.getDeadlineTallyBoundary(ctx, proposal.VotingEndTime)
	if !found {
		return 0, false
	}
	return boundary.UpdateSequence, true
}

func (keeper Keeper) setProposalTallyBoundary(ctx sdk.Context, proposalID uint64, boundaryID []byte) {
	ctx.KVStore(keeper.storeKey).Set(types.ProposalTallyBoundaryKey(proposalID), boundaryID)
}

func (keeper Keeper) setTallyBoundary(ctx sdk.Context, boundaryID []byte, boundary tallyBoundary) {
	bz, err := json.Marshal(boundary)
	if err != nil {
		panic(fmt.Errorf("marshal tally boundary: %w", err))
	}
	ctx.KVStore(keeper.storeKey).Set(types.TallyBoundaryMetaKey(boundaryID), bz)
}

func (keeper Keeper) getTallyBoundary(ctx sdk.Context, boundaryID []byte) (tallyBoundary, bool) {
	bz := ctx.KVStore(keeper.storeKey).Get(types.TallyBoundaryMetaKey(boundaryID))
	if bz == nil {
		return tallyBoundary{}, false
	}
	var boundary tallyBoundary
	if err := json.Unmarshal(bz, &boundary); err != nil {
		panic(fmt.Errorf("unmarshal tally boundary: %w", err))
	}
	return boundary, true
}

func (keeper Keeper) addProposalDeadline(ctx sdk.Context, proposalID uint64, endTime time.Time) {
	if !keeper.IncrementalTallyEnabled(ctx) || keeper.isLegacyProposal(ctx, proposalID) {
		return
	}
	ctx.KVStore(keeper.storeKey).Set(types.ProposalDeadlineKey(proposalID, endTime), []byte{1})
}

func (keeper Keeper) addModernProposalDeadline(ctx sdk.Context, proposalID uint64, endTime time.Time) {
	if !keeper.IncrementalTallyEnabled(ctx) {
		return
	}
	ctx.KVStore(keeper.storeKey).Set(types.ProposalDeadlineKey(proposalID, endTime), []byte{1})
}

func (keeper Keeper) removeProposalDeadline(ctx sdk.Context, proposalID uint64, endTime time.Time) {
	if !keeper.IncrementalTallyEnabled(ctx) {
		return
	}
	store := ctx.KVStore(keeper.storeKey)
	boundary, boundaryID, found := keeper.getSelectedTallyBoundary(ctx, proposalID)
	if !found {
		boundary, boundaryID, found = keeper.getDeadlineTallyBoundary(ctx, endTime)
	}
	store.Delete(types.ProposalDeadlineKey(proposalID, endTime))
	store.Delete(types.ProposalTallyBoundaryKey(proposalID))
	if !found {
		return
	}

	switch boundaryID[0] {
	case proposalTallyBoundary:
		store.Delete(types.TallyBoundaryMetaKey(boundaryID))
	case exactTallyBoundary:
		if !keeper.hasProposalDeadlineAt(ctx, boundary.UpperTime) {
			store.Delete(types.ExactTallyBoundaryKey(boundary.UpperTime))
			store.Delete(types.TallyBoundaryMetaKey(boundaryID))
		}
	case gapTallyBoundary:
		if !keeper.hasProposalDeadlineBetween(ctx, boundary.LowerTime, boundary.UpperTime) {
			store.Delete(types.GapTallyBoundaryKey(boundary.UpperTime))
			store.Delete(types.TallyBoundaryMetaKey(boundaryID))
		}
	default:
		panic(fmt.Sprintf("unknown tally boundary %q", boundaryID))
	}
}

func (keeper Keeper) hasProposalDeadlineAt(ctx sdk.Context, endTime time.Time) bool {
	prefix := types.ProposalDeadlineByTimeKey(endTime)
	iterator := ctx.KVStore(keeper.storeKey).Iterator(prefix, sdk.PrefixEndBytes(prefix))
	defer func() { _ = iterator.Close() }()
	return iterator.Valid()
}

func (keeper Keeper) hasProposalDeadlineBetween(ctx sdk.Context, lowerTime, upperTime time.Time) bool {
	start := sdk.PrefixEndBytes(types.ProposalDeadlineByTimeKey(lowerTime))
	end := types.ProposalDeadlineByTimeKey(upperTime)
	iterator := ctx.KVStore(keeper.storeKey).Iterator(start, end)
	defer func() { _ = iterator.Close() }()
	return iterator.Valid()
}

func gapTallyBoundaryID(upperTime time.Time) []byte {
	return append([]byte{gapTallyBoundary}, sdk.FormatTimeBytes(upperTime)...)
}

func exactTallyBoundaryID(endTime time.Time) []byte {
	return append([]byte{exactTallyBoundary}, sdk.FormatTimeBytes(endTime)...)
}

func proposalSpecificTallyBoundaryID(proposalID uint64) []byte {
	return append([]byte{proposalTallyBoundary}, types.GetProposalIDBytes(proposalID)...)
}

func parseBoundaryTime(value []byte) time.Time {
	blockTime, err := sdk.ParseTimeBytes(value)
	if err != nil {
		panic(fmt.Errorf("parse tally boundary block time: %w", err))
	}
	return blockTime
}

func tallyElectorateToGenesis(proposalID uint64, electorate tallyElectorate) types.TallyElectorate {
	validators := make([]types.TallyValidator, 0, len(electorate.Validators))
	for _, validator := range electorate.Validators {
		validators = append(validators, types.TallyValidator{
			Address:         validator.Address,
			BondedTokens:    validator.BondedTokens,
			DelegatorShares: validator.DelegatorShares,
		})
	}
	return types.TallyElectorate{
		ProposalId:        proposalID,
		TotalBondedTokens: electorate.TotalBondedTokens,
		TallyParams:       electorate.TallyParams,
		TallyValidators:   validators,
	}
}

func tallyElectorateFromGenesis(electorate types.TallyElectorate) tallyElectorate {
	validators := make([]tallyValidator, 0, len(electorate.TallyValidators))
	for _, validator := range electorate.TallyValidators {
		validators = append(validators, tallyValidator{
			Address:                 validator.Address,
			BondedTokens:            validator.BondedTokens,
			DelegatorShares:         validator.DelegatorShares,
			ObservedDelegatorShares: sdk.ZeroDec(),
			DelegatorResults:        newTallyOptionResults(),
		})
	}
	return tallyElectorate{
		TotalBondedTokens: electorate.TotalBondedTokens,
		TallyParams:       electorate.TallyParams,
		Validators:        validators,
	}
}
