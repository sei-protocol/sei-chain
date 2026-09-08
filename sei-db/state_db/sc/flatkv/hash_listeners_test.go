package flatkv

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// tightHashPipelineConfig returns a config that lets only one block wait to be finalized, so that a
// store which stops finalizing wedges within a block or two rather than sixty-four.
func tightHashPipelineConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.DefaultTestConfig(t)
	cfg.FinalizationQueueSize = 1
	return cfg
}

// commitBlocks commits count blocks, each writing one storage slot.
func commitBlocks(t *testing.T, s *CommitStore, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		height := s.Version() + 1
		require.NoError(t, s.ApplyChangeSets(height, []*proto.NamedChangeSet{
			makeChangeSet(evmStorageKey(ktype.Address{0x11}, ktype.Slot{byte(height)}), padLeft32(byte(height)), false),
		}), "apply block %d", height)
		_, err := s.Commit(height)
		require.NoError(t, err, "commit block %d", height)
	}
}

// recordBlocks returns a listener that records the block number of every hash it is handed, and the
// slice it records into. The slice is only safe to read once FlushHashes has returned.
func recordBlocks() (func(context.Context, int64, *lthash.BlockHash) error, *[]int64) {
	blocks := &[]int64{}
	return func(_ context.Context, blockNumber int64, _ *lthash.BlockHash) error {
		*blocks = append(*blocks, blockNumber)
		return nil
	}, blocks
}

// A store hashes every block whether or not anything asked for one, so nobody listening has to be the
// ordinary case rather than something that eventually wedges commit.
func TestCommittingWithNoListeners(t *testing.T) {
	s := setupTestStoreWithConfig(t, tightHashPipelineConfig(t))
	defer func() { require.NoError(t, s.Close()) }()

	const blocks = 16
	commitBlocks(t, s, blocks)

	require.Equal(t, int64(blocks), s.Version())
	require.NoError(t, s.FlushHashes())
}

// The one thing a listener is promised: every block, once, in order. A listener that skipped a block
// could not maintain anything derived from the chain of hashes.
func TestAListenerSeesEveryBlockInOrder(t *testing.T) {
	s := setupTestStoreWithConfig(t, tightHashPipelineConfig(t))
	defer func() { require.NoError(t, s.Close()) }()

	listener, seen := recordBlocks()
	mostRecent, err := s.RegisterHashListener(listener)
	require.NoError(t, err)
	require.Equal(t, int64(0), mostRecent.BlockNumber, "a fresh store has hashed nothing")

	const blocks = 8
	commitBlocks(t, s, blocks)
	require.NoError(t, s.FlushHashes())

	require.Equal(t, []int64{1, 2, 3, 4, 5, 6, 7, 8}, *seen)
}

// FlushHashes is how a caller waits for hashing to catch up, and a hash that has been computed but
// not handed on has not caught up as far as a listener is concerned.
func TestFlushHashesWaitsForEveryBlockToBeDispatched(t *testing.T) {
	s := setupTestStoreWithConfig(t, tightHashPipelineConfig(t))
	defer func() { require.NoError(t, s.Close()) }()

	listener, seen := recordBlocks()
	_, err := s.RegisterHashListener(listener)
	require.NoError(t, err)

	const blocks = 4
	commitBlocks(t, s, blocks)
	require.NoError(t, s.FlushHashes())

	// Read with nothing polling and no second flush: if dispatch were still in flight this would be
	// short, and the assertion would be the one thing standing between that and a silent gap.
	require.Len(t, *seen, blocks)
}

// The hash reported at registration is what tells a caller where the listener picks up. Reporting the
// height the store has reached rather than the height it has dispatched would hide a block.
func TestRegisterReportsTheBlockTheFirstDeliveryFollows(t *testing.T) {
	s := setupTestStoreWithConfig(t, tightHashPipelineConfig(t))
	defer func() { require.NoError(t, s.Close()) }()

	commitBlocks(t, s, 3)
	require.NoError(t, s.FlushHashes())

	listener, seen := recordBlocks()
	mostRecent, err := s.RegisterHashListener(listener)
	require.NoError(t, err)
	require.Equal(t, int64(3), mostRecent.BlockNumber)
	require.Equal(t, rootHash(s), checksumOf(mostRecent.Global))

	commitBlocks(t, s, 2)
	require.NoError(t, s.FlushHashes())

	require.Equal(t, []int64{4, 5}, *seen, "a listener starts at the block after the one it was told")
}

// Listeners are independent: one of them consuming a hash must not take it away from another.
func TestEveryListenerSeesEveryBlock(t *testing.T) {
	s := setupTestStoreWithConfig(t, tightHashPipelineConfig(t))
	defer func() { require.NoError(t, s.Close()) }()

	first, seenByFirst := recordBlocks()
	second, seenBySecond := recordBlocks()
	_, err := s.RegisterHashListener(first)
	require.NoError(t, err)
	_, err = s.RegisterHashListener(second)
	require.NoError(t, err)

	commitBlocks(t, s, 3)
	require.NoError(t, s.FlushHashes())

	require.Equal(t, []int64{1, 2, 3}, *seenByFirst)
	require.Equal(t, []int64{1, 2, 3}, *seenBySecond)
}

// A listener that refuses a block is a caller that cannot keep up with the state it is deriving. The
// store has no way to make that good, so it stops rather than carrying on past it.
func TestAListenerThatFailsBricksTheStore(t *testing.T) {
	s := setupTestStoreWithConfig(t, tightHashPipelineConfig(t))
	defer func() { _ = s.Close() }()

	_, err := s.RegisterHashListener(func(context.Context, int64, *lthash.BlockHash) error {
		return fmt.Errorf("injected listener failure")
	})
	require.NoError(t, err)

	commitBlocks(t, s, 1)

	require.ErrorContains(t, s.FlushHashes(), "injected listener failure",
		"a caller waiting for hashes must be told a listener refused one")

	height := s.Version() + 1
	require.NoError(t, s.ApplyChangeSets(height, []*proto.NamedChangeSet{
		makeChangeSet(evmStorageKey(ktype.Address{0x11}, ktype.Slot{0x99}), padLeft32(0x99), false),
	}))
	_, err = s.Commit(height)
	require.ErrorContains(t, err, "injected listener failure",
		"a store whose listener failed must refuse the next block rather than commit past it")
}

// A caller that wants the current hash and no deliveries passes nil. Refusing it would make such a
// caller invent a callback it has no use for.
func TestANilHashListenerRegistersNothing(t *testing.T) {
	s := setupTestStoreWithConfig(t, tightHashPipelineConfig(t))
	defer func() { require.NoError(t, s.Close()) }()

	commitBlocks(t, s, 2)
	require.NoError(t, s.FlushHashes())

	mostRecent, err := s.RegisterHashListener(nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), mostRecent.BlockNumber, "a nil listener still reports the current hash")

	// Nothing was registered, so the block below has nobody to deliver to and must still commit.
	commitBlocks(t, s, 1)
	require.NoError(t, s.FlushHashes())
	require.Equal(t, int64(3), s.Version())
}

// A rollback rebuilds the hash pipeline underneath the listeners. They belong to the store rather
// than to that pipeline, so they survive it — a rollback that silently dropped a listener would leave
// whatever it feeds frozen at the pre-rollback height.
//
// A rollback re-delivers the heights it replays on its way back to the target, so the listener sees
// them a second time. Nothing observes that in practice — see RegisterHashListener.
func TestRegistrationsSurviveARollback(t *testing.T) {
	s := setupTestStoreWithConfig(t, tightHashPipelineConfig(t))
	defer func() { require.NoError(t, s.Close()) }()

	listener, seen := recordBlocks()
	_, err := s.RegisterHashListener(listener)
	require.NoError(t, err)

	commitBlocks(t, s, 5)
	require.NoError(t, s.FlushHashes())
	require.Equal(t, []int64{1, 2, 3, 4, 5}, *seen)

	require.NoError(t, s.Rollback(3))

	commitBlocks(t, s, 2)
	require.NoError(t, s.FlushHashes())

	require.Equal(t, []int64{1, 2, 3, 4, 5, 1, 2, 3, 4, 5}, *seen,
		"the listener registered before the rollback must still be given the blocks after it")
}

// Each database records the hash it was handed in the same batch as the block's data, so the hash a
// listener is given and the hash on disk are the same claim about the same block. They are compared
// here because a listener acting on one while the store persists the other would be undetectable.
func TestDispatchedPerDBHashesMatchWhatEachDatabaseRecorded(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	var dispatched *lthash.BlockHash
	_, err := s.RegisterHashListener(func(_ context.Context, _ int64, hash *lthash.BlockHash) error {
		dispatched = hash
		return nil
	})
	require.NoError(t, err)

	commitBlocks(t, s, 1)
	require.NoError(t, s.FlushHashes())
	require.NotNil(t, dispatched, "the committed block must have been dispatched")

	require.Equal(t, rootHash(s), checksumOf(dispatched.Global))

	// Read back off disk rather than from the store's load-time copy: the finalizer writes it, so disk
	// is the only place the two can be compared.
	require.NoError(t, s.reloadLocalMeta())
	for _, dir := range dataDBDirs {
		require.Equal(t, checksumOf(s.localMeta[dir].LtHash), checksumOf(dispatched.PerDB[dir]),
			"the %s hash dispatched must be the one that database recorded", dir)
	}

	// Homomorphic invariant: the per-DB LtHashes sum to the dispatched global LtHash.
	sum := lthash.New()
	for _, dir := range dataDBDirs {
		sum.MixIn(s.localMeta[dir].LtHash)
	}
	require.True(t, sum.Equal(dispatched.Global))
}

// checksumOf returns an LtHash's checksum as a slice, for comparison.
func checksumOf(hash *lthash.LtHash) []byte {
	checksum := hash.Checksum()
	return checksum[:]
}
