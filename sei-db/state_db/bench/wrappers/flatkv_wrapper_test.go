package wrappers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// TestFlatKVWrapperBatchesMultipleBlocksBeforeCommit reproduces the
// cryptosim.Database.FinalizeBlock pattern used when BlocksPerCommit > 1:
// ApplyChangeSets is called once per block (each time computing
// entry.Version as wrapper.Version()+1) and Commit is only called every
// N blocks. Regression test for "flatkv: cannot apply at height N; pending
// writes already stamped at N-1".
func TestFlatKVWrapperBatchesMultipleBlocksBeforeCommit(t *testing.T) {
	wrapper, err := NewDBImpl(t.Context(), FlatKV, t.TempDir(), nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, wrapper.Close()) }()

	const blocksPerCommit = 5
	for block := 1; block <= blocksPerCommit; block++ {
		key := []byte("key")
		value := []byte{byte(block)}
		entry := &proto.ChangelogEntry{
			Version: wrapper.Version() + 1,
			Changesets: []*proto.NamedChangeSet{{
				Name:      EVMStoreName,
				Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: key, Value: value}}},
			}},
		}
		require.NoError(t, wrapper.ApplyChangeSets(entry), "block %d", block)
		require.Equal(t, int64(block), wrapper.Version(), "working version after applying block %d", block)
	}

	committed, err := wrapper.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(blocksPerCommit), committed)
	require.Equal(t, int64(blocksPerCommit), wrapper.Version())

	// A subsequent single-block cycle (BlocksPerCommit == 1 shape) must
	// still work after a batched commit.
	entry := &proto.ChangelogEntry{
		Version: wrapper.Version() + 1,
		Changesets: []*proto.NamedChangeSet{{
			Name:      EVMStoreName,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("key2"), Value: []byte{1}}}},
		}},
	}
	require.NoError(t, wrapper.ApplyChangeSets(entry))
	committed, err = wrapper.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(blocksPerCommit+1), committed)
}
