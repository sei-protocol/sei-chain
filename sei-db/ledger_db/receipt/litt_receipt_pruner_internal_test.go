package receipt

import (
	"testing"

	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/stretchr/testify/require"
)

type pruneIndexSpy struct {
	dbtypes.KeyValueDB

	directSets         int
	directSetOptions   []dbtypes.WriteOptions
	directRangeDeletes int
	batchSets          int
	batchRangeDeletes  int
	batchCommits       int
	batchCommitOptions []dbtypes.WriteOptions
}

func (s *pruneIndexSpy) Set(key, value []byte, opts dbtypes.WriteOptions) error {
	s.directSets++
	s.directSetOptions = append(s.directSetOptions, opts)
	return s.KeyValueDB.Set(key, value, opts)
}

func (s *pruneIndexSpy) DeleteRange(start, end []byte, opts dbtypes.WriteOptions) error {
	s.directRangeDeletes++
	return s.KeyValueDB.(interface {
		DeleteRange(start, end []byte, opts dbtypes.WriteOptions) error
	}).DeleteRange(start, end, opts)
}

func (s *pruneIndexSpy) NewBatch() dbtypes.Batch {
	return &pruneBatchSpy{Batch: s.KeyValueDB.NewBatch(), parent: s}
}

type pruneBatchSpy struct {
	dbtypes.Batch
	parent *pruneIndexSpy
}

func (b *pruneBatchSpy) Set(key, value []byte) error {
	b.parent.batchSets++
	return b.Batch.Set(key, value)
}

func (b *pruneBatchSpy) DeleteRange(start, end []byte) error {
	b.parent.batchRangeDeletes++
	return b.Batch.(interface {
		DeleteRange(start, end []byte) error
	}).DeleteRange(start, end)
}

func (b *pruneBatchSpy) Commit(opts dbtypes.WriteOptions) error {
	b.parent.batchCommits++
	b.parent.batchCommitOptions = append(b.parent.batchCommitOptions, opts)
	return b.Batch.Commit(opts)
}

// Which driver enforces retention is a four-way decision, and getting it wrong in either direction
// is a production bug rather than a test nicety: two pruners race to different floors, and none
// lets the tag index grow without bound. Enumerated here rather than observed through the jittered
// ticker so every combination is covered without waiting on any of them.
func TestRunsLocalPruner(t *testing.T) {
	for _, tc := range []struct {
		name            string
		externalPruning bool
		keepRecent      int64
		pruneInterval   int64
		want            bool
	}{
		{
			name:          "standalone node prunes itself",
			keepRecent:    100_000,
			pruneInterval: 600,
			want:          true,
		},
		{
			name:            "under the collector it stands down",
			externalPruning: true,
			keepRecent:      100_000,
			pruneInterval:   600,
			want:            false,
		},
		{
			// KeepRecent 0 is the default and means keep everything, so there is nothing for a
			// local pruner to do. It reaches none of the collector's answers either.
			name:          "keep everything",
			keepRecent:    0,
			pruneInterval: 600,
			want:          false,
		},
		{
			name:          "no cadence configured",
			keepRecent:    100_000,
			pruneInterval: 0,
			want:          false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &littReceiptStore{
				externalPruning: tc.externalPruning,
				keepRecent:      tc.keepRecent,
				pruneInterval:   tc.pruneInterval,
			}
			require.Equal(t, tc.want, s.runsLocalPruner())
		})
	}
}

// The invariant behind the table: with retention configured at all, the local pruner is exactly the
// negation of ExternalPruning. Both on means two pruners racing to different floors; both off means
// nothing advances the floor. Asserted against the method the collector calls, so it covers the two
// staying wired to each other and not merely today's values.
func TestRunsLocalPrunerIsTheNegationOfExternalPruning(t *testing.T) {
	for _, external := range []bool{false, true} {
		s := &littReceiptStore{externalPruning: external, keepRecent: 100_000, pruneInterval: 600}
		require.Equal(t, !s.ExternalPruning(), s.runsLocalPruner(),
			"exactly one of the collector and the local pruner may enforce retention")
	}
}

// The range tombstone and the earliest-version metadata are one logical retention-floor update.
// If they are separate Pebble writes, a crash can preserve the tombstone but lose the metadata:
// after restart RPC reports old blocks as available even though their tag index is gone. One batch
// makes the update all-or-nothing under the KeyValueDB crash contract.
func TestPruneBlocksBelowAtomicallyMovesIndexAndFloor(t *testing.T) {
	s, closeFn := setupLittCtxStore(t)
	defer closeFn()

	s.latestVersion.Store(3)
	s.latestDurableVersion.Store(3)
	spy := &pruneIndexSpy{KeyValueDB: s.index}
	s.index = spy

	require.NoError(t, s.pruneBlocksBelow(3))
	require.Zero(t, spy.directRangeDeletes, "the range tombstone must not commit before the floor metadata")
	require.Zero(t, spy.directSets, "the floor metadata must not be a separate write")
	require.Equal(t, 1, spy.batchRangeDeletes)
	require.Equal(t, 1, spy.batchSets)
	require.Equal(t, 1, spy.batchCommits)
	require.Equal(t, []dbtypes.WriteOptions{{Sync: true}}, spy.batchCommitOptions)
	require.Equal(t, int64(3), s.EarliestVersion())
}

func TestSetEarliestVersionPersistsBeforePublishing(t *testing.T) {
	s, closeFn := setupLittCtxStore(t)
	defer closeFn()

	spy := &pruneIndexSpy{KeyValueDB: s.index}
	s.index = spy

	require.NoError(t, s.SetEarliestVersion(3))
	require.Equal(t, []dbtypes.WriteOptions{{Sync: true}}, spy.directSetOptions)
	require.Equal(t, int64(3), s.EarliestVersion())
}

func TestReceiptGCFlushDoesNotPromoteRecoveredIndexOnlySuffix(t *testing.T) {
	dir := t.TempDir()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = receiptBackendLittIdx
	cfg.DBDirectory = dir
	cfg.ExternalPruning = true
	storeKey := storetypes.NewKVStoreKey("evm")

	store, err := newLittReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	s := store.(*littReceiptStore)
	stopLittIdxBackground(s)
	require.NoError(t, s.index.Set(
		receiptLatestVersionKey,
		encodeBlockNumber(4),
		dbtypes.WriteOptions{Sync: true},
	))
	require.NoError(t, s.index.Set(
		receiptLatestDurableVersionKey,
		encodeBlockNumber(3),
		dbtypes.WriteOptions{Sync: true},
	))
	require.NoError(t, s.Close())

	store, err = newLittReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	s = store.(*littReceiptStore)
	stopLittIdxBackground(s)
	defer func() { require.NoError(t, s.Close()) }()

	require.Equal(t, int64(4), s.LatestVersion())
	require.NoError(t, s.flushReceipts())
	latest, err := s.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(3), latest,
		"flushing after reopen must not certify an index-only suffix left by a prior crash")

	require.NoError(t, s.SetLatestVersion(5))
	require.NoError(t, s.flushReceipts())
	latest, err = s.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(5), latest, "new writes must still advance the durable marker")
}

func stopLittIdxBackground(s *littReceiptStore) {
	close(s.stopBackground)
	s.backgroundWg.Wait()
	s.stopBackground = make(chan struct{})
}
