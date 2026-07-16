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
	s, err := NewCommitStore(t.Context(), cfg)
	require.NoError(t, err)
	ro, err := s.LoadVersion(0, readOnly)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	cs := ro.(*CommitStore)
	defer func() { require.NoError(t, cs.Close()) }()
	return cs.CommittedRootHash()
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
		require.NoError(t, s.ApplyChangeSets(emptyValueBankCS(pairs...)))
		_, err := s.Commit()
		require.NoError(t, err)
	}

	liveRoot := s.CommittedRootHash()
	dir := s.config.DataDir

	require.NoError(t, s.Close())

	roRoot := reopenCommittedRoot(t, dir, true)
	require.Equal(t, liveRoot, roRoot,
		"WAL-replay (read-only clone) committed root must match the live store; "+
			"empty-value keys must not be dropped on replay")
}
