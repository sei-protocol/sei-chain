package flatkv

// Regression test for the empty-value WAL-replay drop.
//
// A changeset pair with Delete=false and an empty (zero-length) value is a
// legitimate "set this key to an empty value" write. Protobuf cannot
// distinguish an empty []byte{} from nil, so after a WAL round-trip the value
// arrives as nil. ApplyChangeSets previously treated a nil value as a deletion,
// so such keys were silently dropped on any replay path (catchup, read-only
// clone, snapshot export, state-sync restore), diverging the committed LtHash
// from the live chain and breaking flatkv_only state sync.

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/stretchr/testify/require"
)

func emptyValueBankCS(pairs ...*proto.KVPair) []*proto.NamedChangeSet {
	return []*proto.NamedChangeSet{{
		Name:      "bank",
		Changeset: proto.ChangeSet{Pairs: pairs},
	}}
}

func reopenCommittedRoot(t *testing.T, dir string, readOnly bool) []byte {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.DataDir = dir
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	if !readOnly {
		require.NoError(t, s.LoadLatest())
		defer func() { require.NoError(t, s.Close()) }()
		return s.PublishedHash().Hash
	}
	ro, err := s.LoadVersionReadOnly(0)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	cs := ro.(*CommitStore)
	defer func() { require.NoError(t, cs.Close()) }()
	return cs.PublishedHash().Hash
}

// TestEmptyValueSurvivesWALReplay drives a key set to an empty value, then
// reconstructs the store from the WAL (read-only clone / catchup) and asserts
// the committed root hash matches the live store. Before the fix the WAL replay
// dropped the empty-value key and produced a different root.
func TestEmptyValueSurvivesWALReplay(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 0
	s := setupTestStoreWithConfig(t, cfg)

	blocks := [][]*proto.KVPair{
		{{Key: []byte("supply"), Value: []byte("S1")}},
		{{Key: []byte("empty-marker"), Value: []byte{}}}, // set empty value
		{{Key: []byte("supply"), Value: []byte("S3")}},
	}
	for _, pairs := range blocks {
		require.NoError(t, s.ApplyChangeSets(s.Version()+1, emptyValueBankCS(pairs...)))
		_, err := s.Commit(s.Version() + 1)
		require.NoError(t, err)
	}

	// The hasher runs behind the commits above, so the live store's root only describes the last block once
	// it has caught up.
	require.NoError(t, s.FlushHashes())
	liveRoot := s.PublishedHash().Hash
	dir := s.config.DataDir

	require.NoError(t, s.Close())

	roRoot := reopenCommittedRoot(t, dir, true)
	require.Equal(t, liveRoot, roRoot,
		"WAL-replay (read-only clone) committed root must match the live store; "+
			"empty-value keys must not be dropped on replay")
}

func TestEmptyValueVisibleBeforeCommit(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	s := setupTestStoreWithConfig(t, cfg)
	defer func() { require.NoError(t, s.Close()) }()

	key := []byte("empty-marker")
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, emptyValueBankCS(&proto.KVPair{
		Key:   key,
		Value: []byte{},
	})))

	value, found := s.Get("bank", key)
	require.True(t, found)
	require.NotNil(t, value, "empty value is present data, not absence")
	require.Empty(t, value)
	require.True(t, s.Has("bank", key))

	iter, err := s.Iterator("bank", nil, nil, true)
	require.NoError(t, err)
	require.True(t, iter.Valid())
	require.Equal(t, key, iter.Key())
	require.NotNil(t, iter.Value(), "pending iterator should preserve empty value presence")
	require.Empty(t, iter.Value())
	iter.Next()
	require.False(t, iter.Valid())
	require.NoError(t, iter.Error())
	require.NoError(t, iter.Close())

	_, err = s.Commit(s.Version() + 1)
	require.NoError(t, err)

	value, found = s.Get("bank", key)
	require.True(t, found)
	require.NotNil(t, value)
	require.Empty(t, value)
	require.True(t, s.Has("bank", key))
}
