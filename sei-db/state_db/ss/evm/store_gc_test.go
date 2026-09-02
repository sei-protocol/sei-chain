package evm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// openPrunableTestStore opens a store with retention handed to the collector and its snapshot manager
// started, which is the shape a Giga node runs it in.
func openPrunableTestStore(t *testing.T) *EVMStateStore {
	t.Helper()
	cfg := testConfig()
	cfg.ExternalPruning = true

	store, err := NewEVMStateStore(t.TempDir(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.StartSnapshots(utils.GetStateStoreSnapshotsSiblingPath(store.Dir()), cfg, nil))
	return store
}

// applyVersion writes one key at version so the store has a head to measure against.
func applyVersion(t *testing.T, store *EVMStateStore, version int64) {
	t.Helper()
	key := append([]byte{0x0a}, make([]byte, 20)...)
	require.NoError(t, store.ApplyChangesetSync(version, []*proto.NamedChangeSet{{
		Name:      EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: key, Value: []byte{0x01}}}},
	}}))
}

// The collector prunes this store only when it says so, and it says so from the config it was opened
// with. A store left reporting false receives neither prune call.
func TestExternalPruningComesFromTheConfig(t *testing.T) {
	require.True(t, openPrunableTestStore(t).ExternalPruning())

	store, err := NewEVMStateStore(t.TempDir(), testConfig())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.False(t, store.ExternalPruning())
}

// A history cut line above this store's own head is clamped to the head rather than acted on. The cut
// line is a minimum across every store on the node, so it can name a height this one has not reached;
// pruning to it unclamped would drop every version the store holds and leave it answering no
// historical query at all.
func TestPruneHistoryIsClampedToTheHead(t *testing.T) {
	store := openPrunableTestStore(t)
	applyVersion(t, store, 5)

	require.NoError(t, store.PruneHistory(1000))

	head, err := store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(5), head, "the head must survive a cut line above it")
	require.LessOrEqual(t, store.GetEarliestVersion(), int64(5),
		"nothing at or above the head may be dropped")
}

// A store that has ingested nothing has nothing to prune, and must not be asked to prune below a
// height it has never held.
func TestPruneHistoryOnAnEmptyStoreDoesNothing(t *testing.T) {
	store := openPrunableTestStore(t)

	require.NoError(t, store.PruneHistory(0))
	require.NoError(t, store.PruneHistory(100))
	require.Zero(t, store.GetEarliestVersion())
}

// The floor is what holds the shared cut line down to a height this store can still restore to.
// Without a snapshot to restore from it holds the line at 0 — keep everything — rather than letting
// the cut line move above the history it still needs.
func TestRollbackFloorHoldsAtZeroWithoutASnapshot(t *testing.T) {
	store := openPrunableTestStore(t)
	applyVersion(t, store, 100)

	require.Zero(t, store.GetRollbackFloor(10))
}

// A store checkpointing on a schedule reports its floor from the snapshot the schedule produced, which
// is what makes the two halves of pruning agree on one height.
func TestRollbackFloorNamesThePublishedSnapshot(t *testing.T) {
	store := openPrunableTestStore(t)
	store.SetCheckpointScheduler(controller.NewCheckpointScheduler(dbconfig.CheckpointConfig{BlockInterval: 1}))

	applyVersion(t, store, 10)
	store.ScheduleSnapshot(10)
	// ScheduleSnapshot returns once the checkpoint is queued, so the publication it will be followed by
	// is already registered and waiting here cannot miss it.
	store.checkpoint.publishing.Wait()
	require.Equal(t, int64(10), store.Snapshots().Newest())

	applyVersion(t, store, 60)
	require.Equal(t, uint64(10), store.GetRollbackFloor(20),
		"the only snapshot is the deepest height a restore can start from")
	require.Zero(t, store.GetRollbackFloor(100),
		"a window deeper than the whole history makes nothing eligible for pruning")
}

// Until the store has a schedule it takes no snapshot at all, so a node that forgot to wire one is a
// node with no restore point rather than one quietly snapshotting on a cadence of its own.
func TestNoSnapshotWithoutASchedule(t *testing.T) {
	store := openPrunableTestStore(t)

	applyVersion(t, store, 10)
	store.ScheduleSnapshot(10)
	store.WaitForPendingWrites()

	require.Zero(t, store.Snapshots().Newest())
}
