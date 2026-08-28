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

	store := ctx.KVStore(m.keeper.storeKey)
	store.Set(types.VoteDelegationBackfillCutoffKey, types.GetProposalIDBytes(nextProposalID))
	return nil
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

func (keeper Keeper) voteNeedsDelegationBackfill(ctx sdk.Context, proposalID uint64) bool {
	store := ctx.KVStore(keeper.storeKey)
	cutoff := store.Get(types.VoteDelegationBackfillCutoffKey)
	if cutoff == nil {
		return false
	}
	if len(cutoff) != 8 {
		panic("invalid vote delegation backfill cutoff")
	}
	if proposalID >= types.GetProposalIDFromBytes(cutoff) {
		return false
	}
	progress := store.Get(types.VoteDelegationBackfillProgressKey(proposalID))
	return !voteDelegationBackfillIsComplete(progress)
}

func voteDelegationBackfillIsComplete(progress []byte) bool {
	return len(progress) == 1 && progress[0] == voteDelegationBackfillComplete
}
