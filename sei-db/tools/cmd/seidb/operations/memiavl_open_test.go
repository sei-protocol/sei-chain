package operations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
)

// TestOpenMemiAVLReplayReadOnlyRefusesADirectoryAWriterHasOpen covers what no
// check of the directory can: the changelog opener repairs a torn tail and
// completes an interrupted truncation, and both conditions occur transiently
// while a writer appends or truncates. Replaying a directory a writer holds is
// refused instead.
func TestOpenMemiAVLReplayReadOnlyRefusesADirectoryAWriterHasOpen(t *testing.T) {
	homeDir := t.TempDir()
	store := newTestMemiavlStore(t, homeDir)
	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addrN(0xA1), 1)}},
	}}))
	_, err := store.Commit(store.Version() + 1)
	require.NoError(t, err)

	// The store is still open, holding the lock the way a running seid does.
	dbDir := utils.GetCosmosSCStorePath(homeDir)
	db, err := openMemiAVLReplayReadOnly(dbDir, 0)
	require.Nil(t, db)
	require.ErrorIs(t, err, memiavl.ErrLocked)
	require.Contains(t, err.Error(), "stop seid and rerun")
	require.Contains(t, err.Error(), "--memiavl-open-mode snapshot")

	// The same directory opens once that writer releases it, so the refusal
	// tracks the writer rather than something the first open left behind.
	require.NoError(t, store.Close())
	db, err = openMemiAVLReplayReadOnly(dbDir, 0)
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

// TestOpenMemiAVLReplayReadOnlyReplaysAStoppedNode is the positive control: the
// lock is the only thing the refusal adds, so a released directory replays as
// it did before.
func TestOpenMemiAVLReplayReadOnlyReplaysAStoppedNode(t *testing.T) {
	homeDir := t.TempDir()
	store := newTestMemiavlStore(t, homeDir)
	for nonce := uint64(1); nonce <= 3; nonce++ {
		require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
			Name:      keys.EVMStoreKey,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addrN(0xA1), nonce)}},
		}}))
		_, err := store.Commit(store.Version() + 1)
		require.NoError(t, err)
	}
	require.NoError(t, store.Close())

	db, err := openMemiAVLReplayReadOnly(utils.GetCosmosSCStorePath(homeDir), 3)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.Equal(t, int64(3), db.Version())
}
