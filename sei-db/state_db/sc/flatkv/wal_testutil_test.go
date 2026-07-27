package flatkv

import (
	"context"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/stretchr/testify/require"
)

// newCommitStoreWithWAL constructs a CommitStore with a real state WAL opened from cfg's changelog
// directory — the test-suite equivalent of how the composite wires production stores. It stands in for the
// former 2-arg NewCommitStore calls now that the state WAL is an injected constructor argument; the suite's
// call sites were mechanically rewritten to use it.
func newCommitStoreWithWAL(ctx context.Context, cfg *config.Config) (*CommitStore, error) {
	stateWAL, err := OpenStateWAL(cfg)
	if err != nil {
		return nil, err
	}
	return NewCommitStore(ctx, cfg, stateWAL)
}

// walBlockNumbers returns the block numbers stored in the store's WAL, in ascending order. Test-only.
func walBlockNumbers(t *testing.T, s *CommitStore) []uint64 {
	t.Helper()
	ok, first, last, err := s.wal.GetStoredRange()
	require.NoError(t, err)
	if !ok {
		return nil
	}
	it, err := s.wal.Iterator(first, last)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	var blocks []uint64
	for {
		hasNext, err := it.Next()
		require.NoError(t, err)
		if !hasNext {
			break
		}
		block, _ := it.Entry()
		blocks = append(blocks, block)
	}
	return blocks
}

// singleWALBlockChangesets returns the changesets of the sole block stored in the store's WAL, asserting
// there is exactly one. Test-only.
func singleWALBlockChangesets(t *testing.T, s *CommitStore) []*proto.NamedChangeSet {
	t.Helper()
	ok, first, last, err := s.wal.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok, "expected a WAL block")
	require.Equal(t, first, last, "expected exactly one WAL block")

	it, err := s.wal.Iterator(first, last)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	hasNext, err := it.Next()
	require.NoError(t, err)
	require.True(t, hasNext)
	_, changesets := it.Entry()
	return changesets
}
