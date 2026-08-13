package wrappers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

func flatKVEntry(version int64, value byte) *proto.ChangelogEntry {
	return &proto.ChangelogEntry{
		Version: version,
		Changesets: []*proto.NamedChangeSet{{
			Name:      EVMStoreName,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("key"), Value: []byte{value}}}},
		}},
	}
}

// TestFlatKVWrapperCommitsOneBlockPerCommit drives the cryptosim Database.FinalizeBlock pattern: each block
// is applied at Version()+1 and committed immediately. It runs several cycles because the first block is the
// one case a broken version calculation still gets right.
//
// The benchmark's flatkv store has no state WAL, so the contiguity this pins is flatkv's own — Commit refuses
// any version that is not committed+1. The WAL's identical rule is covered by the flatkv package's tests.
func TestFlatKVWrapperCommitsOneBlockPerCommit(t *testing.T) {
	wrapper, err := NewDBImpl(t.Context(), FlatKV, t.TempDir(), nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, wrapper.Close()) }()

	for block := 1; block <= 5; block++ {
		require.NoError(t,
			wrapper.ApplyChangeSets(flatKVEntry(wrapper.Version()+1, byte(block))), "block %d", block)
		committed, err := wrapper.Commit()
		require.NoError(t, err, "block %d", block)
		require.Equal(t, int64(block), committed)
		require.Equal(t, int64(block), wrapper.Version())
	}
}

// TestFlatKVWrapperRejectsSecondBlockBeforeCommit pins the barrier against
// batched blocks: FlatKV persists exactly one block per Commit, and the refusal
// lands on the offending ApplyChangeSets rather than on a later Commit.
func TestFlatKVWrapperRejectsSecondBlockBeforeCommit(t *testing.T) {
	wrapper, err := NewDBImpl(t.Context(), FlatKV, t.TempDir(), nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, wrapper.Close()) }()

	require.NoError(t, wrapper.ApplyChangeSets(flatKVEntry(1, 0x01)))

	err = wrapper.ApplyChangeSets(flatKVEntry(2, 0x02))
	require.ErrorContains(t, err, "flatkv: apply version 2 must be committed version 0 plus one")

	// The rejected call left the pending block intact and committable.
	committed, err := wrapper.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), committed)
}
