package composite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func bankChangeset(key, value string) []*proto.NamedChangeSet {
	return []*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{{Key: []byte(key), Value: []byte(value)}},
			},
		},
	}
}

func newTestCheckpointManager(store *CompositeStateStore, root string) *checkpointManager {
	return &checkpointManager{
		store:      store,
		root:       root,
		backend:    "pebbledb",
		interval:   5,
		keepRecent: 1,
		quit:       make(chan struct{}),
	}
}

// TestCheckpointCreateAndReopen exercises the full online-checkpoint cycle:
// write through the async path, checkpoint while nothing is quiesced, and
// reopen the checkpoint as a standalone store that must contain every version
// up to the label.
func TestCheckpointCreateAndReopen(t *testing.T) {
	store, dir, cleanup := setupTestStores(t)
	defer cleanup()

	addr := make([]byte, 20)
	slot := make([]byte, 32)
	storageKey := append([]byte{0x03}, append(addr, slot...)...)
	for v := int64(1); v <= 10; v++ {
		require.NoError(t, store.ApplyChangesetAsync(v, []*proto.NamedChangeSet{
			{
				Name: "bank",
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{{Key: []byte("balance"), Value: []byte{byte(v)}}},
				},
			},
			{
				Name: "evm",
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{{Key: storageKey, Value: []byte{byte(v)}}},
				},
			},
		}))
	}

	root := filepath.Join(dir, "data", "state_store", CheckpointsDirName)
	m := newTestCheckpointManager(store, root)

	// Mirror the manager's ordering: read the label BEFORE the barrier.
	label := store.minAppliedVersion()
	require.NoError(t, m.createCheckpoint(label))

	name := CheckpointDirName(label)
	target, err := os.Readlink(filepath.Join(root, checkpointCurrentLink))
	require.NoError(t, err)
	require.Equal(t, name, target)

	versions, err := ListCheckpointVersions(root)
	require.NoError(t, err)
	require.Equal(t, []int64{label}, versions)

	// Reopen the checkpoint as its own store; every version <= label must be
	// present and readable.
	ckptDir := filepath.Join(root, name)
	reopened, err := NewCompositeStateStore(config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		EVMSplit:         true,
		DBDirectory:      filepath.Join(ckptDir, "cosmos", "pebbledb"),
		EVMDBDirectory:   filepath.Join(ckptDir, "evm", "pebbledb"),
	}, t.TempDir())
	require.NoError(t, err)
	defer reopened.Close()

	require.GreaterOrEqual(t, reopened.GetLatestVersion(), label)
	for v := int64(1); v <= label; v++ {
		val, err := reopened.Get("bank", v, []byte("balance"))
		require.NoError(t, err)
		require.Equal(t, []byte{byte(v)}, val, "cosmos version %d missing from checkpoint", v)
		val, err = reopened.Get("evm", v, storageKey)
		require.NoError(t, err)
		require.Equal(t, []byte{byte(v)}, val, "evm version %d missing from checkpoint", v)
	}
}

// TestCheckpointCompleteBelowEmptyBlockMarker pins the barrier ordering: an
// empty block writes the latest-version marker directly to Pebble while older
// changesets may still sit in the async queue. A checkpoint labeled with that
// marker must nevertheless contain every data version below it.
func TestCheckpointCompleteBelowEmptyBlockMarker(t *testing.T) {
	store, dir, cleanup := setupTestStores(t)
	defer cleanup()

	for v := int64(1); v <= 5; v++ {
		require.NoError(t, store.ApplyChangesetAsync(v, bankChangeset("k", string(rune('a'+v)))))
	}
	// Empty block 6: marker only, bypasses the queue.
	require.NoError(t, store.SetLatestVersion(6))

	root := filepath.Join(dir, "data", "state_store", CheckpointsDirName)
	m := newTestCheckpointManager(store, root)
	label := store.minAppliedVersion()
	require.Equal(t, int64(6), label)
	require.NoError(t, m.createCheckpoint(label))

	ckptDir := filepath.Join(root, CheckpointDirName(label))
	reopened, err := NewCompositeStateStore(config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		DBDirectory:      filepath.Join(ckptDir, "cosmos", "pebbledb"),
	}, t.TempDir())
	require.NoError(t, err)
	defer reopened.Close()

	// The on-disk latest marker may legitimately read 5 rather than 6: each
	// applied batch stamps the marker with its own version, so the queued v5
	// batch (drained by the barrier) can overwrite the empty block 6's
	// direct marker write. That understatement only ever spans trailing
	// EMPTY versions — no data is missing — and the first block applied
	// after a restore pushes the marker forward again.
	require.GreaterOrEqual(t, reopened.GetLatestVersion(), int64(5))
	val, err := reopened.Get("bank", 5, []byte("k"))
	require.NoError(t, err)
	require.Equal(t, []byte(string(rune('a'+5))), val,
		"data version below the empty-block marker must be inside the checkpoint")
}

// TestCheckpointPrune verifies retention: with keepRecent=1, only the newest
// two checkpoints survive and current tracks the newest.
func TestCheckpointPrune(t *testing.T) {
	store, dir, cleanup := setupTestStores(t)
	defer cleanup()

	root := filepath.Join(dir, "data", "state_store", CheckpointsDirName)
	m := newTestCheckpointManager(store, root)

	for _, label := range []int64{10, 20, 30} {
		require.NoError(t, store.ApplyChangesetSync(label, bankChangeset("k", "v")))
		require.NoError(t, m.createCheckpoint(label))
	}

	versions, err := ListCheckpointVersions(root)
	require.NoError(t, err)
	require.Equal(t, []int64{20, 30}, versions)

	target, err := os.Readlink(filepath.Join(root, checkpointCurrentLink))
	require.NoError(t, err)
	require.Equal(t, CheckpointDirName(30), target)
}

func TestParseCheckpointVersion(t *testing.T) {
	v, ok := ParseCheckpointVersion(CheckpointDirName(219140000))
	require.True(t, ok)
	require.Equal(t, int64(219140000), v)

	for _, bad := range []string{"snapshot-", "snapshot-123", "current", "tmp-snapshot-00000000000000000010", "snapshot-0000000000000000001x"} {
		_, ok := ParseCheckpointVersion(bad)
		require.False(t, ok, "expected %q to be rejected", bad)
	}
}
