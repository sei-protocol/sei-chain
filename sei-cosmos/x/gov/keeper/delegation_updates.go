package keeper

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
)

type pendingVoteDelegationUpdate struct {
	Voter     string    `json:"voter"`
	Validator string    `json:"validator"`
	Shares    sdk.Dec   `json:"shares"`
	BlockTime time.Time `json:"block_time"`
	Cursor    []byte    `json:"cursor,omitempty"`
}

// QueueVoteDelegationUpdate defers one slash-induced delegation change for bounded processing.
func (keeper Keeper) QueueVoteDelegationUpdate(
	ctx sdk.Context,
	voter sdk.AccAddress,
	validator sdk.ValAddress,
	shares sdk.Dec,
) {
	if !keeper.IncrementalTallyEnabled(ctx) || !keeper.voterHasTrackedProposals(ctx, voter) {
		return
	}

	sequence := keeper.nextVoteDelegationUpdateSequence(ctx)
	update := pendingVoteDelegationUpdate{
		Voter:     voter.String(),
		Validator: validator.String(),
		Shares:    shares,
		BlockTime: ctx.BlockTime(),
	}
	store := ctx.KVStore(keeper.storeKey)
	store.Set(types.VoteDelegationUpdateKey(sequence), marshalVoteDelegationUpdate(update))
	store.Set(types.VoterVoteDelegationUpdateKey(voter, sequence), []byte{1})
}

// ProcessVoteDelegationUpdates applies at most maxUpdates deferred snapshot updates.
func (keeper Keeper) ProcessVoteDelegationUpdates(ctx sdk.Context, maxUpdates int) (complete bool, processed int) {
	return keeper.ProcessVoteDelegationUpdatesThrough(ctx, maxUpdates, math.MaxUint64)
}

// ProcessVoteDelegationUpdatesThrough applies deferred snapshot updates through a sequence.
func (keeper Keeper) ProcessVoteDelegationUpdatesThrough(
	ctx sdk.Context,
	maxUpdates int,
	throughSequence uint64,
) (complete bool, processed int) {
	if maxUpdates < 0 {
		panic("maximum vote delegation updates cannot be negative")
	}
	if !keeper.IncrementalTallyEnabled(ctx) {
		return true, 0
	}

	store := ctx.KVStore(keeper.storeKey)
	iterator := sdk.KVStorePrefixIterator(store, types.VoteDelegationUpdatesKeyPrefix)
	defer func() { _ = iterator.Close() }()

	for ; iterator.Valid() && processed < maxUpdates; iterator.Next() {
		key := append([]byte(nil), iterator.Key()...)
		sequence := voteDelegationUpdateSequenceFromKey(key)
		if sequence > throughSequence {
			return true, processed
		}
		update := unmarshalVoteDelegationUpdate(iterator.Value())
		var updateComplete bool
		updateComplete, processed = keeper.processVoteDelegationUpdate(
			ctx,
			sequence,
			update,
			maxUpdates,
			processed,
		)
		if !updateComplete {
			return false, processed
		}
		store.Delete(key)
		voter := sdk.MustAccAddressFromBech32(update.Voter)
		store.Delete(types.VoterVoteDelegationUpdateKey(voter, sequence))
	}

	return !iterator.Valid() || voteDelegationUpdateSequenceFromKey(iterator.Key()) > throughSequence, processed
}

// HasPendingVoteDelegationUpdates reports whether slash-induced snapshot work remains.
func (keeper Keeper) HasPendingVoteDelegationUpdates(ctx sdk.Context) bool {
	if !keeper.IncrementalTallyEnabled(ctx) {
		return false
	}
	iterator := sdk.KVStorePrefixIterator(ctx.KVStore(keeper.storeKey), types.VoteDelegationUpdatesKeyPrefix)
	defer func() { _ = iterator.Close() }()
	return iterator.Valid()
}

func (keeper Keeper) voterHasTrackedProposals(ctx sdk.Context, voter sdk.AccAddress) bool {
	iterator := sdk.KVStorePrefixIterator(
		ctx.KVStore(keeper.storeKey),
		types.VoterProposalsKeyPrefixForAddress(voter),
	)
	defer func() { _ = iterator.Close() }()
	return iterator.Valid()
}

func (keeper Keeper) nextVoteDelegationUpdateSequence(ctx sdk.Context) uint64 {
	store := ctx.KVStore(keeper.storeKey)
	sequence := decodeVoteDelegationUpdateSequence(store.Get(types.VoteDelegationUpdateSequenceKey))
	if sequence == math.MaxUint64 {
		panic("vote delegation update sequence overflow")
	}
	sequence++
	store.Set(types.VoteDelegationUpdateSequenceKey, types.GetProposalIDBytes(sequence))
	return sequence
}

func (keeper Keeper) processVoteDelegationUpdate(
	ctx sdk.Context,
	sequence uint64,
	update pendingVoteDelegationUpdate,
	maxUpdates int,
	processed int,
) (complete bool, newProcessed int) {
	store := ctx.KVStore(keeper.storeKey)
	voter := sdk.MustAccAddressFromBech32(update.Voter)
	prefix := types.VoterProposalsKeyPrefixForAddress(voter)
	start := prefix
	if len(update.Cursor) != 0 {
		start = sdk.PrefixEndBytes(append(append([]byte(nil), prefix...), update.Cursor...))
	}
	iterator := store.Iterator(start, sdk.PrefixEndBytes(prefix))
	defer func() { _ = iterator.Close() }()

	newProcessed = processed
	for ; iterator.Valid() && newProcessed < maxUpdates; iterator.Next() {
		proposalIDBytes := iterator.Key()[len(prefix):]
		if len(proposalIDBytes) != 8 {
			panic(fmt.Sprintf("invalid voter proposal key length %d", len(iterator.Key())))
		}
		proposalID := types.GetProposalIDFromBytes(proposalIDBytes)
		keeper.applyVoteDelegationUpdate(ctx, proposalID, sequence, update)
		update.Cursor = append(update.Cursor[:0], proposalIDBytes...)
		newProcessed++
	}

	if iterator.Valid() {
		store.Set(types.VoteDelegationUpdateKey(sequence), marshalVoteDelegationUpdate(update))
		return false, newProcessed
	}
	if newProcessed == processed {
		newProcessed++
	}
	return true, newProcessed
}

func (keeper Keeper) applyVoteDelegationUpdate(
	ctx sdk.Context,
	proposalID uint64,
	sequence uint64,
	update pendingVoteDelegationUpdate,
) {
	voter := sdk.MustAccAddressFromBech32(update.Voter)
	if keeper.voteDelegationSnapshotRevision(ctx, proposalID, voter) >= sequence {
		return
	}

	proposal, found := keeper.GetProposal(ctx, proposalID)
	if found && keeper.delegationUpdateBelongsToTallyBoundary(ctx, proposal, sequence, update.BlockTime) {
		store := ctx.KVStore(keeper.storeKey)
		snapshotKey := types.VoteDelegationsKey(proposalID, voter)
		bz := store.Get(snapshotKey)
		if bz == nil {
			panic(fmt.Sprintf("missing delegation snapshot for proposal %d voter %s", proposalID, voter))
		}
		snapshot := keeper.unmarshalVoteDelegations(bz)
		index := newVoteDelegationSnapshotIndex(snapshot)
		index.set(update.Validator, update.Shares)
		keeper.storeVoteDelegationSnapshot(ctx, index.snapshot(), sequence)
		return
	}
	keeper.setVoteDelegationSnapshotRevision(ctx, proposalID, voter, sequence)
}

func (keeper Keeper) applyVoteDelegationSnapshotUpdates(
	ctx sdk.Context,
	proposalID uint64,
	voter sdk.AccAddress,
	snapshot types.VoteDelegationSnapshot,
) types.VoteDelegationSnapshot {
	index := newVoteDelegationSnapshotIndex(snapshot)

	proposal, found := keeper.GetProposal(ctx, proposalID)
	if !found {
		return index.snapshot()
	}

	revision := keeper.voteDelegationSnapshotRevision(ctx, proposalID, voter)
	prefix := types.VoterVoteDelegationUpdatesKeyPrefixForAddress(voter)
	start := prefix
	if revision != 0 {
		start = sdk.PrefixEndBytes(append(append([]byte(nil), prefix...), types.GetProposalIDBytes(revision)...))
	}
	store := ctx.KVStore(keeper.storeKey)
	iterator := store.Iterator(start, sdk.PrefixEndBytes(prefix))
	defer func() { _ = iterator.Close() }()
	for ; iterator.Valid(); iterator.Next() {
		sequence := voteDelegationUpdateSequenceFromVoterKey(iterator.Key(), len(prefix))
		bz := store.Get(types.VoteDelegationUpdateKey(sequence))
		if bz == nil {
			panic(fmt.Sprintf("missing vote delegation update %d", sequence))
		}
		update := unmarshalVoteDelegationUpdate(bz)
		if keeper.delegationUpdateBelongsToTallyBoundary(ctx, proposal, sequence, update.BlockTime) {
			index.set(update.Validator, update.Shares)
		}
	}
	return index.snapshot()
}

func (keeper Keeper) delegationUpdateBelongsToTallyBoundary(
	ctx sdk.Context,
	proposal types.Proposal,
	sequence uint64,
	updateTime time.Time,
) bool {
	if boundarySequence, found := keeper.proposalTallyBoundarySequence(ctx, proposal); found {
		return sequence <= boundarySequence
	}
	if keeper.usesLegacyTallySemantics(ctx, proposal) {
		return !keeper.IsTallying(ctx, proposal.ProposalId)
	}
	return !proposal.VotingEndTime.Before(updateTime) && !keeper.IsTallying(ctx, proposal.ProposalId)
}

func (keeper Keeper) deleteVoteDelegationSnapshotRevision(ctx sdk.Context, proposalID uint64, voter sdk.AccAddress) {
	ctx.KVStore(keeper.storeKey).Delete(types.VoteDelegationSnapshotRevisionKey(proposalID, voter))
}

func (keeper Keeper) voteDelegationUpdateSequence(ctx sdk.Context) uint64 {
	return decodeVoteDelegationUpdateSequence(
		ctx.KVStore(keeper.storeKey).Get(types.VoteDelegationUpdateSequenceKey),
	)
}

func (keeper Keeper) voteDelegationSnapshotRevision(ctx sdk.Context, proposalID uint64, voter sdk.AccAddress) uint64 {
	return decodeVoteDelegationUpdateSequence(
		ctx.KVStore(keeper.storeKey).Get(types.VoteDelegationSnapshotRevisionKey(proposalID, voter)),
	)
}

func (keeper Keeper) setVoteDelegationSnapshotRevision(
	ctx sdk.Context,
	proposalID uint64,
	voter sdk.AccAddress,
	revision uint64,
) {
	ctx.KVStore(keeper.storeKey).Set(
		types.VoteDelegationSnapshotRevisionKey(proposalID, voter),
		types.GetProposalIDBytes(revision),
	)
}

func marshalVoteDelegationUpdate(update pendingVoteDelegationUpdate) []byte {
	bz, err := json.Marshal(update)
	if err != nil {
		panic(fmt.Errorf("marshal vote delegation update: %w", err))
	}
	return bz
}

func unmarshalVoteDelegationUpdate(bz []byte) pendingVoteDelegationUpdate {
	var update pendingVoteDelegationUpdate
	if err := json.Unmarshal(bz, &update); err != nil {
		panic(fmt.Errorf("unmarshal vote delegation update: %w", err))
	}
	return update
}

func decodeVoteDelegationUpdateSequence(bz []byte) uint64 {
	if bz == nil {
		return 0
	}
	if len(bz) != 8 {
		panic(fmt.Sprintf("invalid vote delegation update sequence length %d", len(bz)))
	}
	return binary.BigEndian.Uint64(bz)
}

func voteDelegationUpdateSequenceFromKey(key []byte) uint64 {
	if len(key) != len(types.VoteDelegationUpdatesKeyPrefix)+8 {
		panic(fmt.Sprintf("invalid vote delegation update key length %d", len(key)))
	}
	return binary.BigEndian.Uint64(key[len(types.VoteDelegationUpdatesKeyPrefix):])
}

func voteDelegationUpdateSequenceFromVoterKey(key []byte, prefixLength int) uint64 {
	if len(key) != prefixLength+8 {
		panic(fmt.Sprintf("invalid voter vote delegation update key length %d", len(key)))
	}
	return binary.BigEndian.Uint64(key[prefixLength:])
}

type voteDelegationSnapshotIndex struct {
	value     types.VoteDelegationSnapshot
	positions map[string]int
}

func newVoteDelegationSnapshotIndex(snapshot types.VoteDelegationSnapshot) *voteDelegationSnapshotIndex {
	positions := make(map[string]int, len(snapshot.Delegations))
	for i, delegation := range snapshot.Delegations {
		positions[delegation.Validator] = i
	}
	return &voteDelegationSnapshotIndex{value: snapshot, positions: positions}
}

func (index *voteDelegationSnapshotIndex) set(validator string, shares sdk.Dec) {
	if position, found := index.positions[validator]; found {
		index.value.Delegations[position].Shares = shares
		return
	}
	if shares.IsZero() {
		return
	}
	index.positions[validator] = len(index.value.Delegations)
	index.value.Delegations = append(index.value.Delegations, types.VoteDelegation{
		Validator: validator,
		Shares:    shares,
	})
}

func (index *voteDelegationSnapshotIndex) snapshot() types.VoteDelegationSnapshot {
	delegations := index.value.Delegations[:0]
	for _, delegation := range index.value.Delegations {
		if !delegation.Shares.IsZero() {
			delegations = append(delegations, delegation)
		}
	}
	index.value.Delegations = delegations
	return index.value
}
