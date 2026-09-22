package keeper

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
)

const voteDelegationBackfillComplete byte = 0

// Migrator is a struct for handling in-place store migrations.
type Migrator struct {
	keeper Keeper
}

// NewMigrator returns a new Migrator.
func NewMigrator(keeper Keeper) Migrator {
	return Migrator{keeper: keeper}
}

// Migrate1to2 migrates from version 1 to 2.
func (m Migrator) Migrate1to2(ctx sdk.Context) error {
	return nil
}

// Migrate2to3 migrates from version 2 to 3.
func (m Migrator) Migrate2to3(ctx sdk.Context) error {
	return nil
}

// Migrate3to4 schedules delegation-tracking backfill for existing votes.
func (m Migrator) Migrate3to4(ctx sdk.Context) error {
	nextProposalID, err := m.keeper.GetProposalID(ctx)
	if err != nil {
		return err
	}

	m.keeper.SetVoteDelegationBackfillCutoff(ctx, nextProposalID)
	m.keeper.EnableIncrementalTally(ctx)
	m.keeper.InitializeDeadlineBoundaryClock(ctx)
	return nil
}

// EnableIncrementalTally records that bounded governance tallying is active.
func (keeper Keeper) EnableIncrementalTally(ctx sdk.Context) {
	ctx.KVStore(keeper.storeKey).Set(types.IncrementalTallyEnabledKey, []byte{1})
}

// SetVoteDelegationBackfillCutoff records the first proposal that does not require delegation-tracking backfill.
func (keeper Keeper) SetVoteDelegationBackfillCutoff(ctx sdk.Context, proposalID uint64) {
	ctx.KVStore(keeper.storeKey).Set(types.VoteDelegationBackfillCutoffKey, types.GetProposalIDBytes(proposalID))
}

// GetVoteDelegationBackfillCutoff returns the first proposal that does not require delegation-tracking backfill.
func (keeper Keeper) GetVoteDelegationBackfillCutoff(ctx sdk.Context) (uint64, bool) {
	cutoff := ctx.KVStore(keeper.storeKey).Get(types.VoteDelegationBackfillCutoffKey)
	if cutoff == nil {
		return 0, false
	}
	if len(cutoff) != 8 {
		panic("invalid vote delegation backfill cutoff")
	}
	return types.GetProposalIDFromBytes(cutoff), true
}

// BackfillVoteDelegationTracking initializes tracking for at most maxVotes of a proposal's votes.
func (keeper Keeper) BackfillVoteDelegationTracking(
	ctx sdk.Context,
	proposalID uint64,
	maxVotes int,
) (complete bool, processed int) {
	if maxVotes < 0 {
		panic("maximum votes to backfill cannot be negative")
	}
	if !keeper.voteNeedsDelegationBackfill(ctx, proposalID) {
		return true, 0
	}

	store := ctx.KVStore(keeper.storeKey)
	progressKey := types.VoteDelegationBackfillProgressKey(proposalID)
	cursor := store.Get(progressKey)
	if cursor == nil {
		cursor = types.VotesKey(proposalID)
		store.Set(progressKey, cursor)
	}

	votesPrefix := types.VotesKey(proposalID)
	iterator := store.Iterator(cursor, sdk.PrefixEndBytes(votesPrefix))
	defer func() { _ = iterator.Close() }()

	for ; iterator.Valid() && processed < maxVotes; iterator.Next() {
		_, voter := types.SplitKeyVote(iterator.Key())
		if !store.Has(types.VoteDelegationsKey(proposalID, voter)) ||
			!store.Has(types.VoterProposalsKey(voter, proposalID)) {
			keeper.initializeVoteDelegationTracking(ctx, proposalID, voter)
		}
		processed++
	}

	if iterator.Valid() {
		store.Set(progressKey, append([]byte(nil), iterator.Key()...))
		return false, processed
	}
	store.Set(progressKey, []byte{voteDelegationBackfillComplete})
	return true, processed
}

// IsVoteDelegationBackfillInProgress reports whether a proposal's tracking backfill has started but not finished.
func (keeper Keeper) IsVoteDelegationBackfillInProgress(ctx sdk.Context, proposalID uint64) bool {
	progress := ctx.KVStore(keeper.storeKey).Get(types.VoteDelegationBackfillProgressKey(proposalID))
	return progress != nil && !voteDelegationBackfillIsComplete(progress)
}

// VoteDelegationBackfillRequired reports whether a proposal's votes require delegation-tracking backfill.
func (keeper Keeper) VoteDelegationBackfillRequired(ctx sdk.Context, proposalID uint64) bool {
	return keeper.voteNeedsDelegationBackfill(ctx, proposalID)
}

// CompleteVoteDelegationBackfill marks a legacy proposal's delegation tracking complete.
func (keeper Keeper) CompleteVoteDelegationBackfill(ctx sdk.Context, proposalID uint64) {
	if !keeper.isLegacyProposal(ctx, proposalID) || keeper.IsModernTallyRound(ctx, proposalID) {
		return
	}
	ctx.KVStore(keeper.storeKey).Set(
		types.VoteDelegationBackfillProgressKey(proposalID),
		[]byte{voteDelegationBackfillComplete},
	)
}

func (keeper Keeper) voteNeedsDelegationBackfill(ctx sdk.Context, proposalID uint64) bool {
	if !keeper.isLegacyProposal(ctx, proposalID) || keeper.IsModernTallyRound(ctx, proposalID) {
		return false
	}
	progress := ctx.KVStore(keeper.storeKey).Get(types.VoteDelegationBackfillProgressKey(proposalID))
	return !voteDelegationBackfillIsComplete(progress)
}

func (keeper Keeper) isLegacyProposal(ctx sdk.Context, proposalID uint64) bool {
	cutoff, found := keeper.GetVoteDelegationBackfillCutoff(ctx)
	if !found {
		return false
	}
	return proposalID < cutoff
}

func (keeper Keeper) usesLegacyTallySemantics(ctx sdk.Context, proposal types.Proposal) bool {
	return keeper.isLegacyProposal(ctx, proposal.ProposalId) && !keeper.IsModernTallyRound(ctx, proposal.ProposalId)
}

// SetModernTallyRound marks a legacy proposal's converted regular round for deadline-based tallying.
func (keeper Keeper) SetModernTallyRound(ctx sdk.Context, proposalID uint64) {
	ctx.KVStore(keeper.storeKey).Set(types.ModernTallyRoundKey(proposalID), []byte{1})
}

// IsModernTallyRound reports whether a legacy proposal's current round uses deadline-based tallying.
func (keeper Keeper) IsModernTallyRound(ctx sdk.Context, proposalID uint64) bool {
	return ctx.KVStore(keeper.storeKey).Has(types.ModernTallyRoundKey(proposalID))
}

func voteDelegationBackfillIsComplete(progress []byte) bool {
	return len(progress) == 1 && progress[0] == voteDelegationBackfillComplete
}
