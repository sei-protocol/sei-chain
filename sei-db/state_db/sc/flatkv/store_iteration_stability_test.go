package flatkv

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

// This file pins the flatKV half of one property: an iterator serves the store's state as of the
// instant it was created, for as long as it is held, and holding one does not stop the store
// committing blocks.
//
// It matters more here than one layer down, because a flatKV EVM iterator is not one iterator — it is
// four or five, spread across four snapshot engines, stitched together by a merge. They are created
// one after another, so they only add up to a single coherent instant because creation happens under
// the store's own lock. These tests assert the composition, not just the parts.

// evmMiscKey builds a logical EVM key that routes to the misc lane: 0x01 is none of the optimised
// prefixes (storage 0x03, code 0x07, codehash 0x08, nonce 0x0a), so it is preserved whole.
func evmMiscKey(suffix ...byte) []byte {
	return append([]byte{0x01}, suffix...)
}

// touchEveryLane returns a changeset that writes one row into every EVM lane, so a test can prove an
// iterator is unaffected across all of them at once rather than one at a time.
func touchEveryLane(addr byte, nonce uint64, value byte) *proto.NamedChangeSet {
	a := addrN(addr)
	return namedCS(
		noncePair(a, nonce),
		codeHashPair(a, codeHashN(value)),
		codePair(a, []byte{value}),
		storagePair(a, slotN(0x01), []byte{value}),
		&proto.KVPair{Key: evmMiscKey(addr), Value: []byte{value}},
	)
}

// applyAndCommitBlock stages a changeset as the next block and commits it.
func applyAndCommitBlock(t *testing.T, s *CommitStore, cs *proto.NamedChangeSet) {
	t.Helper()
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{cs}))
	_, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
}

// iteratorTwin opens two iterators back to back with no write in between, so both describe the same
// instant. The first is returned for the caller to hold across the writes it wants to prove are
// invisible; the second is drained immediately to capture what that instant contained.
//
// Two are needed because a dbm.Iterator is single-pass: the same one cannot be read before and after.
func iteratorTwin(t *testing.T, open func() (dbm.Iterator, error)) (dbm.Iterator, []evmIteratorEntry) {
	t.Helper()
	held, err := open()
	require.NoError(t, err)
	probe, err := open()
	require.NoError(t, err)
	before := collectIterEntries(t, probe)
	require.NoError(t, probe.Close())
	require.NotEmpty(t, before, "the committed rows must be visible to a fresh iterator")
	return held, before
}

// An EVM iterator spans several lanes over four engines. Committing a block that rewrites every one
// of those lanes must be accepted, and must not be visible through any of them.
func TestEvmIteratorSurvivesCommitAcrossEveryLane(t *testing.T) {
	s := setupTestStore(t)
	defer func() { _ = s.Close() }()

	applyAndCommitBlock(t, s, touchEveryLane(0x01, 7, 0xaa))

	iter, before := iteratorTwin(t, func() (dbm.Iterator, error) {
		return s.Iterator(keys.EVMStoreKey, nil, nil, true)
	})

	// Rewrite every lane's row and add a second address, while the iterator is held.
	applyAndCommitBlock(t, s, touchEveryLane(0x01, 99, 0xbb))
	applyAndCommitBlock(t, s, touchEveryLane(0x02, 1, 0xcc))

	require.Equal(t, before, collectIterEntries(t, iter),
		"every lane must still serve the instant the iterator was created")
	require.NoError(t, iter.Close())

	// A fresh iterator sees the new state, so the store really did move on.
	after, err := s.Iterator(keys.EVMStoreKey, nil, nil, true)
	require.NoError(t, err)
	require.Greater(t, len(collectIterEntries(t, after)), len(before))
	require.NoError(t, after.Close())
}

// The same property for a single non-EVM module, which is one lane over the misc engine.
func TestModuleIteratorSurvivesCommit(t *testing.T) {
	s := setupTestStore(t)
	defer func() { _ = s.Close() }()

	const module = "bank"
	applyAndCommitBlock(t, s, &proto.NamedChangeSet{
		Name:      module,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("k1"), Value: []byte("v1")}}},
	})

	iter, before := iteratorTwin(t, func() (dbm.Iterator, error) {
		return s.Iterator(module, nil, nil, true)
	})
	require.Len(t, before, 1)

	applyAndCommitBlock(t, s, &proto.NamedChangeSet{
		Name: module,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k1"), Value: []byte("clobbered")},
			{Key: []byte("k2"), Value: []byte("v2")},
		}},
	})

	require.Equal(t, before, collectIterEntries(t, iter))
	require.NoError(t, iter.Close())
}

// The global iterator merges all four data engines with no bounds. It refuses to open while a block
// is staged, so it is opened between blocks; from then on it must be unaffected by later ones.
func TestRawGlobalIteratorSurvivesCommit(t *testing.T) {
	s := setupTestStore(t)
	defer func() { _ = s.Close() }()

	applyAndCommitBlock(t, s, touchEveryLane(0x01, 7, 0xaa))

	iter, before := iteratorTwin(t, s.RawGlobalIterator)

	applyAndCommitBlock(t, s, touchEveryLane(0x01, 99, 0xbb))
	applyAndCommitBlock(t, s, touchEveryLane(0x03, 3, 0xdd))

	require.Equal(t, before, collectIterEntries(t, iter))
	require.NoError(t, iter.Close())
}

// Holding an iterator across the periodic snapshot is the flatKV-level version of surviving a flush:
// the snapshot path waits for the committed block to reach the databases before checkpointing them,
// so the iterator's data is written out from under it while it is still being read.
func TestEvmIteratorSurvivesAutoSnapshot(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 2
	s := setupTestStoreWithConfig(t, cfg)
	defer func() { _ = s.Close() }()

	applyAndCommitBlock(t, s, touchEveryLane(0x01, 7, 0xaa))

	iter, before := iteratorTwin(t, func() (dbm.Iterator, error) {
		return s.Iterator(keys.EVMStoreKey, nil, nil, true)
	})

	// Block 2 trips the snapshot interval, which forces a flush of everything committed so far. The
	// snapshot is written off the execution thread, so wait for it: without that this test drains the
	// iterator before the checkpoint has touched the databases, and stops testing anything.
	applyAndCommitBlock(t, s, touchEveryLane(0x01, 99, 0xbb))
	require.Equal(t, int64(2), s.Version())
	require.NoError(t, s.FlushSnapshots())

	require.Equal(t, before, collectIterEntries(t, iter))
	require.NoError(t, iter.Close())
}

// Enough blocks that the engines flush and retire the version the iterator copied from, so its data
// has physically moved out of the engines' in-memory maps by the time it is drained.
func TestEvmIteratorSurvivesRetirement(t *testing.T) {
	s := setupTestStore(t)
	defer func() { _ = s.Close() }()

	applyAndCommitBlock(t, s, touchEveryLane(0x01, 7, 0xaa))

	iter, before := iteratorTwin(t, func() (dbm.Iterator, error) {
		return s.Iterator(keys.EVMStoreKey, nil, nil, true)
	})

	for i := 0; i < 6; i++ {
		applyAndCommitBlock(t, s, touchEveryLane(byte(0x10+i), uint64(i+1), byte(0x30+i)))
		requireFlushedToDisk(t, s)
	}

	require.Equal(t, before, collectIterEntries(t, iter))
	require.NoError(t, iter.Close())
}

// A bounded range can drop whole lanes, so bounded and reverse iteration are asserted separately.
func TestBoundedAndReverseEvmIteratorsSurviveCommit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		start     []byte
		end       []byte
		ascending bool
	}{
		{"ascending unbounded", nil, nil, true},
		{"descending unbounded", nil, nil, false},
		// Confined to the nonce lane, so most lanes are skipped entirely.
		{"ascending nonce lane only", []byte{0x0a}, []byte{0x0b}, true},
		{"descending nonce lane only", []byte{0x0a}, []byte{0x0b}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := setupTestStore(t)
			defer func() { _ = s.Close() }()

			applyAndCommitBlock(t, s, touchEveryLane(0x01, 7, 0xaa))

			iter, before := iteratorTwin(t, func() (dbm.Iterator, error) {
				return s.Iterator(keys.EVMStoreKey, tc.start, tc.end, tc.ascending)
			})

			applyAndCommitBlock(t, s, touchEveryLane(0x01, 99, 0xbb))
			applyAndCommitBlock(t, s, touchEveryLane(0x02, 1, 0xcc))

			require.Equal(t, before, collectIterEntries(t, iter))
			require.NoError(t, iter.Close())
		})
	}
}

// An iterator held on one goroutine — a query handler, or the state-sync export — while another commits
// blocks. The layer above flatKV turns a failed commit into a process crash, so committing while an
// iterator is held is a hard requirement rather than a convenience.
func TestCommitSucceedsWhileIteratorHeldOnAnotherGoroutine(t *testing.T) {
	s := setupTestStore(t)
	defer func() { _ = s.Close() }()

	applyAndCommitBlock(t, s, touchEveryLane(0x01, 7, 0xaa))

	iter, before := iteratorTwin(t, func() (dbm.Iterator, error) {
		return s.Iterator(keys.EVMStoreKey, nil, nil, true)
	})

	// The reader parks on the iterator while the writer commits, so the commits genuinely overlap a
	// held iterator rather than merely following one.
	release := make(chan struct{})
	var readerDone sync.WaitGroup
	var readEntries []evmIteratorEntry
	readerDone.Add(1)
	go func() {
		defer readerDone.Done()
		<-release
		readEntries = collectIterEntries(t, iter)
	}()

	var committed atomic.Int64
	for i := 0; i < 5; i++ {
		require.NoError(t, s.ApplyChangeSets(s.Version()+1,
			[]*proto.NamedChangeSet{touchEveryLane(byte(0x40+i), uint64(i+1), byte(0x50+i))}),
			"staging block %d must be accepted while an iterator is held", i+1)
		_, err := s.Commit(s.Version() + 1)
		require.NoError(t, err, "committing block %d must be accepted while an iterator is held", i+1)
		committed.Add(1)
	}

	close(release)
	readerDone.Wait()

	require.EqualValues(t, 5, committed.Load())
	require.Equal(t, before, readEntries, "the held iterator must still serve its creation instant")
	require.NoError(t, iter.Close())
}

// Iterators are undefined behaviour once the store has closed. Closing with one open must say so
// rather than closing silently and leaving the holder to walk into a closed database.
func TestCloseReportsOpenIterator(t *testing.T) {
	s := setupTestStore(t)

	applyAndCommitBlock(t, s, touchEveryLane(0x01, 7, 0xaa))

	iter, err := s.Iterator(keys.EVMStoreKey, nil, nil, true)
	require.NoError(t, err)
	t.Cleanup(func() { _ = iter.Close() })

	// The backing database also complains about its own leaked iterators, so this asserts our
	// message specifically: the store must name the leak and say that using it is undefined, rather
	// than leaving the caller to infer it from a storage-engine internal error.
	require.ErrorContains(t, s.Close(), "undefined",
		"closing with an iterator open must name the leak and its consequence")
}
