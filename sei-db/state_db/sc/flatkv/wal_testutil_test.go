package flatkv

import (
	"context"
	"os"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"github.com/stretchr/testify/require"
)

// newCommitStoreWithWAL constructs a CommitStore with a real state WAL opened from cfg's changelog
// directory — the test-suite equivalent of how the composite wires production stores. The WAL is an
// injected constructor argument, so every test that needs a committable store goes through here.
func newCommitStoreWithWAL(ctx context.Context, cfg *config.Config) (*CommitStore, error) {
	stateWAL, err := OpenStateWAL(cfg)
	if err != nil {
		return nil, err
	}
	return NewCommitStore(ctx, cfg, stateWAL)
}

// resetWALForTest closes the store's WAL, removes its directory and reopens an empty one in place, leaving the
// store able to keep committing. It exists so a test can choose exactly which blocks the WAL retains: prune is
// file-granular and asynchronous, so it cannot shape a small WAL deterministically.
//
// Test-only, and deliberately not available in production: a node whose WAL must be discarded (a state-sync
// restore) has its changelog directory removed out of band while the node is stopped.
func resetWALForTest(t *testing.T, s *CommitStore) {
	t.Helper()
	cfg := StateWALConfig(s.config.DataDir)
	require.NoError(t, s.wal.Close())
	require.NoError(t, os.RemoveAll(cfg.Path))
	w, err := statewal.New(cfg)
	require.NoError(t, err)
	s.wal = w
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
