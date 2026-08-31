package keeper

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/cachekv"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// AddVote adds a vote on a specific proposal
func (keeper Keeper) AddVote(ctx sdk.Context, proposalID uint64, voterAddr sdk.AccAddress, options types.WeightedVoteOptions) error {
	proposal, ok := keeper.GetProposal(ctx, proposalID)
	if !ok {
		return sdkerrors.Wrapf(types.ErrUnknownProposal, "%d", proposalID)
	}
	if proposal.Status != types.StatusVotingPeriod {
		return sdkerrors.Wrapf(types.ErrInactiveProposal, "%d", proposalID)
	}
	if keeper.IncrementalTallyEnabled(ctx) {
		if proposal.VotingEndTime.Before(ctx.BlockTime()) {
			return sdkerrors.Wrapf(types.ErrInactiveProposal, "%d", proposalID)
		}
		if keeper.voteDelegationSnapshotFrozen(ctx, proposal) || keeper.IsVoteDelegationBackfillInProgress(ctx, proposalID) {
			return sdkerrors.Wrapf(types.ErrInactiveProposal, "%d", proposalID)
		}
	}

	for _, option := range options {
		if !types.ValidWeightedVoteOption(option) {
			return sdkerrors.Wrap(types.ErrInvalidVote, option.String())
		}
	}

	vote := types.NewVote(proposalID, voterAddr, options)
	keeper.SetVote(ctx, vote)

	// called after a vote on a proposal is cast
	keeper.AfterProposalVote(ctx, proposalID, voterAddr)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeProposalVote,
			sdk.NewAttribute(types.AttributeKeyOption, options.String()),
			sdk.NewAttribute(types.AttributeKeyProposalID, fmt.Sprintf("%d", proposalID)),
		),
	)

	return nil
}

// GetAllVotes returns all the votes from the store
func (keeper Keeper) GetAllVotes(ctx sdk.Context) (votes types.Votes) {
	keeper.IterateAllVotes(ctx, func(vote types.Vote) bool {
		populateLegacyOption(&vote)
		votes = append(votes, vote)
		return false
	})
	return
}

// GetVotes returns all the votes from a proposal
func (keeper Keeper) GetVotes(ctx sdk.Context, proposalID uint64) (votes types.Votes) {
	keeper.IterateVotes(ctx, proposalID, func(vote types.Vote) bool {
		populateLegacyOption(&vote)
		votes = append(votes, vote)
		return false
	})
	return
}

// GetArchivedTallyVotes returns votes already processed by an unfinished proposal tally.
func (keeper Keeper) GetArchivedTallyVotes(ctx sdk.Context, proposalID uint64, expedited bool) (votes types.Votes) {
	store := ctx.KVStore(keeper.storeKey)
	iterator := sdk.KVStorePrefixIterator(store, types.TallyVotesKey(proposalID, expedited))
	defer func() { _ = iterator.Close() }()

	for ; iterator.Valid(); iterator.Next() {
		var vote types.Vote
		keeper.cdc.MustUnmarshal(iterator.Value(), &vote)
		populateLegacyOption(&vote)
		votes = append(votes, vote)
	}
	return votes
}

// GetVote gets the vote from an address on a specific proposal
func (keeper Keeper) GetVote(ctx sdk.Context, proposalID uint64, voterAddr sdk.AccAddress) (vote types.Vote, found bool) {
	store := keeper.visibleVotesStore(ctx, proposalID)
	votesPrefix := types.VotesKey(proposalID)
	voteKey := types.VoteKey(proposalID, voterAddr)
	bz := store.Get(voteKey[len(votesPrefix):])
	if bz == nil {
		return vote, false
	}

	keeper.cdc.MustUnmarshal(bz, &vote)
	populateLegacyOption(&vote)

	return vote, true
}

// SetVote sets a Vote to the gov store
func (keeper Keeper) SetVote(ctx sdk.Context, vote types.Vote) {
	// vote.Option is a deprecated field, we don't set it in state
	if vote.Option != types.OptionEmpty { //nolint
		vote.Option = types.OptionEmpty //nolint
	}

	store := ctx.KVStore(keeper.storeKey)
	bz := keeper.cdc.MustMarshal(&vote)
	addr := sdk.MustAccAddressFromBech32(vote.Voter)

	store.Set(types.VoteKey(vote.ProposalId, addr), bz)
	keeper.initializeVoteDelegationTracking(ctx, vote.ProposalId, addr)
}

func (keeper Keeper) initializeVoteDelegationTracking(ctx sdk.Context, proposalID uint64, voter sdk.AccAddress) {
	if !keeper.IncrementalTallyEnabled(ctx) {
		return
	}
	ctx.KVStore(keeper.storeKey).Set(types.VoterProposalsKey(voter, proposalID), []byte{1})
	snapshot := keeper.snapshotVoteDelegations(ctx, proposalID, voter)
	keeper.setVoteDelegationSnapshot(ctx, snapshot)
}

// IncrementalTallyEnabled reports whether bounded governance tallying is active.
func (keeper Keeper) IncrementalTallyEnabled(ctx sdk.Context) bool {
	activationCtx := ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)).WithTraceMode(ctx.IsTracing())
	return activationCtx.KVStore(keeper.storeKey).Has(types.IncrementalTallyEnabledKey)
}

func (keeper Keeper) snapshotVoteDelegations(
	ctx sdk.Context,
	proposalID uint64,
	voter sdk.AccAddress,
) types.VoteDelegationSnapshot {
	return keeper.snapshotVoteDelegationsExcept(ctx, proposalID, voter, nil)
}

func (keeper Keeper) snapshotVoteDelegationsExcept(
	ctx sdk.Context,
	proposalID uint64,
	voter sdk.AccAddress,
	excludedValidator sdk.ValAddress,
) types.VoteDelegationSnapshot {
	snapshot := types.VoteDelegationSnapshot{
		ProposalId:  proposalID,
		Voter:       voter.String(),
		Delegations: []types.VoteDelegation{},
	}
	keeper.sk.IterateDelegations(ctx, voter, func(_ int64, delegation stakingtypes.DelegationI) bool {
		if excludedValidator != nil && delegation.GetValidatorAddr().Equals(excludedValidator) {
			return false
		}
		snapshot.Delegations = append(snapshot.Delegations, types.VoteDelegation{
			Validator: delegation.GetValidatorAddr().String(),
			Shares:    delegation.GetShares(),
		})
		return false
	})
	return snapshot
}

func (keeper Keeper) refreshVoteDelegationSnapshots(
	ctx sdk.Context,
	voter sdk.AccAddress,
	excludedValidator sdk.ValAddress,
) {
	if !keeper.IncrementalTallyEnabled(ctx) {
		return
	}
	store := ctx.KVStore(keeper.storeKey)
	prefix := types.VoterProposalsKeyPrefixForAddress(voter)
	iterator := sdk.KVStorePrefixIterator(store, prefix)
	defer func() { _ = iterator.Close() }()
	if !iterator.Valid() {
		return
	}

	snapshot := keeper.snapshotVoteDelegationsExcept(ctx, 0, voter, excludedValidator)
	for ; iterator.Valid(); iterator.Next() {
		proposalID := types.GetProposalIDFromBytes(iterator.Key()[len(prefix):])
		proposal, found := keeper.GetProposal(ctx, proposalID)
		if !found || keeper.voteDelegationSnapshotFrozen(ctx, proposal) {
			continue
		}
		snapshot.ProposalId = proposalID
		keeper.setVoteDelegationSnapshot(ctx, snapshot)
	}
}

func (keeper Keeper) voteDelegationSnapshotFrozen(ctx sdk.Context, proposal types.Proposal) bool {
	if _, found := keeper.proposalTallyBoundarySequence(ctx, proposal); found {
		return true
	}
	if keeper.usesLegacyTallySemantics(ctx, proposal) {
		return keeper.IsTallying(ctx, proposal.ProposalId)
	}
	return proposal.VotingEndTime.Before(ctx.BlockTime()) || keeper.IsTallying(ctx, proposal.ProposalId)
}

func (keeper Keeper) setVoteDelegationSnapshot(ctx sdk.Context, snapshot types.VoteDelegationSnapshot) {
	keeper.storeVoteDelegationSnapshot(ctx, snapshot, keeper.voteDelegationUpdateSequence(ctx))
}

func (keeper Keeper) storeVoteDelegationSnapshot(
	ctx sdk.Context,
	snapshot types.VoteDelegationSnapshot,
	revision uint64,
) {
	voter := sdk.MustAccAddressFromBech32(snapshot.Voter)
	bz := keeper.cdc.MustMarshal(&snapshot)
	ctx.KVStore(keeper.storeKey).Set(types.VoteDelegationsKey(snapshot.ProposalId, voter), bz)
	keeper.setVoteDelegationSnapshotRevision(ctx, snapshot.ProposalId, voter, revision)
}

// SetVoteDelegationSnapshot stores a vote's exported delegation snapshot.
func (keeper Keeper) SetVoteDelegationSnapshot(ctx sdk.Context, snapshot types.VoteDelegationSnapshot) {
	keeper.setVoteDelegationSnapshot(ctx, snapshot)
}

// GetVoteDelegationSnapshots returns the stored delegation snapshots for a proposal's visible votes.
func (keeper Keeper) GetVoteDelegationSnapshots(
	ctx sdk.Context,
	proposal types.Proposal,
) []types.VoteDelegationSnapshot {
	snapshots := make([]types.VoteDelegationSnapshot, 0)
	keeper.IterateVotes(ctx, proposal.ProposalId, func(vote types.Vote) bool {
		snapshots = append(snapshots, keeper.voteDelegations(ctx, proposal.ProposalId, proposal.IsExpedited, vote))
		return false
	})
	return snapshots
}

func (keeper Keeper) unmarshalVoteDelegations(bz []byte) types.VoteDelegationSnapshot {
	var snapshot types.VoteDelegationSnapshot
	keeper.cdc.MustUnmarshal(bz, &snapshot)
	return snapshot
}

// IterateAllVotes iterates over the all the stored votes and performs a callback function
func (keeper Keeper) IterateAllVotes(ctx sdk.Context, cb func(vote types.Vote) (stop bool)) {
	store := ctx.KVStore(keeper.storeKey)
	if keeper.IncrementalTallyEnabled(ctx) {
		progressIterator := sdk.KVStorePrefixIterator(store, types.TallyProgressKeyPrefix)
		for ; progressIterator.Valid(); progressIterator.Next() {
			proposalID := types.GetProposalIDFromBytes(progressIterator.Key()[len(types.TallyProgressKeyPrefix):])
			progress, found := keeper.getTallyProgress(ctx, proposalID)
			if !found {
				continue
			}
			if keeper.iterateVoteStore(prefix.NewStore(store, types.TallyVotesKey(proposalID, progress.Expedited)), cb) {
				_ = progressIterator.Close()
				return
			}
		}
		_ = progressIterator.Close()
	}

	iterator := sdk.KVStorePrefixIterator(store, types.VotesKeyPrefix)

	defer func() { _ = iterator.Close() }()
	for ; iterator.Valid(); iterator.Next() {
		var vote types.Vote
		keeper.cdc.MustUnmarshal(iterator.Value(), &vote)
		populateLegacyOption(&vote)

		if cb(vote) {
			break
		}
	}
}

// IterateVotes iterates over the all the proposals votes and performs a callback function
func (keeper Keeper) IterateVotes(ctx sdk.Context, proposalID uint64, cb func(vote types.Vote) (stop bool)) {
	keeper.iterateVoteStore(keeper.visibleVotesStore(ctx, proposalID), cb)
}

func (keeper Keeper) iterateVoteStore(store storetypes.KVStore, cb func(vote types.Vote) (stop bool)) bool {
	iterator := store.Iterator(nil, nil)

	defer func() { _ = iterator.Close() }()
	for ; iterator.Valid(); iterator.Next() {
		var vote types.Vote
		keeper.cdc.MustUnmarshal(iterator.Value(), &vote)
		populateLegacyOption(&vote)

		if cb(vote) {
			return true
		}
	}
	return false
}

func (keeper Keeper) visibleVotesStore(ctx sdk.Context, proposalID uint64) storetypes.KVStore {
	store := ctx.KVStore(keeper.storeKey)
	pending := prefix.NewStore(store, types.VotesKey(proposalID))
	if !keeper.IncrementalTallyEnabled(ctx) {
		return pending
	}
	progress, found := keeper.getTallyProgress(ctx, proposalID)
	if !found {
		return pending
	}

	return visibleVotesStore{
		KVStore:  pending,
		archived: prefix.NewStore(store, types.TallyVotesKey(proposalID, progress.Expedited)),
		storeKey: keeper.storeKey,
	}
}

type visibleVotesStore struct {
	storetypes.KVStore
	archived storetypes.KVStore
	storeKey sdk.StoreKey
}

func (store visibleVotesStore) Get(key []byte) []byte {
	if value := store.KVStore.Get(key); value != nil {
		return value
	}
	return store.archived.Get(key)
}

func (store visibleVotesStore) Has(key []byte) bool {
	return store.KVStore.Has(key) || store.archived.Has(key)
}

func (store visibleVotesStore) Iterator(start, end []byte) storetypes.Iterator {
	return cachekv.NewCacheMergeIterator(
		store.archived.Iterator(start, end),
		store.KVStore.Iterator(start, end),
		true,
		store.storeKey,
	)
}

func (store visibleVotesStore) ReverseIterator(start, end []byte) storetypes.Iterator {
	return cachekv.NewCacheMergeIterator(
		store.archived.ReverseIterator(start, end),
		store.KVStore.ReverseIterator(start, end),
		false,
		store.storeKey,
	)
}

// populateLegacyOption adds graceful fallback of deprecated `Option` field, in case
// there's only 1 VoteOption.
func populateLegacyOption(vote *types.Vote) {
	if len(vote.Options) == 1 && vote.Options[0].Weight.Equal(sdk.MustNewDecFromStr("1.0")) {
		vote.Option = vote.Options[0].Option //nolint
	}
}
