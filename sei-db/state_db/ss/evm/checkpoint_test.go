package evm

import (
	"testing"

	"github.com/stretchr/testify/require"

	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// cosmosChangeset returns a changeset holding nothing this store keeps, which is what a block that
// touched no EVM state hands it.
func cosmosChangeset() []*proto.NamedChangeSet {
	return []*proto.NamedChangeSet{{
		Name:      "bank",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("balance"), Value: []byte{0x01}}}},
	}}
}

// A block that changed no EVM state still moves this store's head and still reaches the schedule, so
// a boundary landing on such a block produces a snapshot instead of slipping to the next boundary.
func TestCommitBlockWithoutEVMChangesReachesTheBoundary(t *testing.T) {
	store := openPrunableTestStore(t)
	store.SetCheckpointScheduler(controller.NewCheckpointScheduler(dbconfig.CheckpointConfig{BlockInterval: 10}))

	applyVersion(t, store, 9)
	require.NoError(t, store.CommitBlock(10, cosmosChangeset()))
	store.checkpoint.publishing.Wait()

	head, err := store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(10), head, "an empty block must still advance the head")
	require.Equal(t, int64(10), store.Snapshots().Newest())
}

// Committing a block is what offers its version, so a store driven through the commit path needs no
// separate trigger to reach a boundary.
func TestCommitBlockOffersTheVersionItApplies(t *testing.T) {
	store := openPrunableTestStore(t)
	store.SetCheckpointScheduler(controller.NewCheckpointScheduler(dbconfig.CheckpointConfig{BlockInterval: 10}))

	require.NoError(t, store.CommitBlock(10, evmChangeset()))
	store.checkpoint.publishing.Wait()

	require.Equal(t, int64(10), store.Snapshots().Newest())
}

// A version reaches the schedule once. A repeat offer would stage a checkpoint the first is still
// writing and report the height early, releasing the schedule to pick another one.
func TestAVersionIsOfferedToTheScheduleOnce(t *testing.T) {
	store := openPrunableTestStore(t)
	store.SetCheckpointScheduler(controller.NewCheckpointScheduler(dbconfig.CheckpointConfig{BlockInterval: 1}))

	require.NotNil(t, store.acceptCheckpointOffer(10))
	require.Nil(t, store.acceptCheckpointOffer(10), "the same version must not be offered twice")
	require.Nil(t, store.acceptCheckpointOffer(9), "a version below the last offer is a repeat too")
	require.NotNil(t, store.acceptCheckpointOffer(11))

	store.SetCheckpointScheduler(nil)
	require.Nil(t, store.acceptCheckpointOffer(20), "a store stood down takes no offers")
}
