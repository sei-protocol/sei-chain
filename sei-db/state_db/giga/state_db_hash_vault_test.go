package giga

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashvault"
)

// vaultTestStores is the set of configs a StateDB under test opens, kept so the test can reopen it.
type vaultTestStores struct {
	// Where SC and the state WAL live.
	flatkvCfg *flatkvconfig.Config

	// SS stays disabled: it produces no hashes, so nothing here depends on it.
	ssCfg config.StateStoreConfig

	// The schedule SC snapshots on when a test does not install one of its own.
	checkpointCfg config.CheckpointConfig

	// Where the hash vault lives, and what it does on a mismatch.
	hashVaultCfg hashvault.HashVaultConfig
}

// newVaultTestStores returns configs for a fresh StateDB whose vault halts on a mismatch.
func newVaultTestStores(t *testing.T) *vaultTestStores {
	t.Helper()
	flatkvCfg := flatkvconfig.DefaultTestConfig(t)
	// The snapshots the rewinds land on are kept, since no collector runs here to prune them.
	flatkvCfg.ExternalPruning = true
	hashVaultCfg := hashvault.DefaultHashVaultConfig()
	hashVaultCfg.DataDir = filepath.Join(t.TempDir(), "hashvault")
	hashVaultCfg.Fsync = false
	return &vaultTestStores{
		flatkvCfg:     flatkvCfg,
		ssCfg:         config.StateStoreConfig{Enable: false},
		checkpointCfg: config.CheckpointConfig{TimeInterval: time.Hour},
		hashVaultCfg:  hashVaultCfg,
	}
}

// open opens the StateDB, failing the test if it cannot.
func (c *vaultTestStores) open(t *testing.T) *StateDB {
	t.Helper()
	db, err := c.openErr()
	require.NoError(t, err)
	return db
}

// openErr opens the StateDB.
func (c *vaultTestStores) openErr() (*StateDB, error) {
	return NewStateDB(context.Background(), c.flatkvCfg, c.ssCfg, c.checkpointCfg, c.hashVaultCfg, 0)
}

// commitBlocks commits blocks first through last, each writing its own value, and snapshots SC at every
// one of them so any of them can be rewound to.
func commitBlocks(t *testing.T, db *StateDB, first int64, last int64) {
	t.Helper()
	require.NoError(t, db.SC().FlushSnapshots())
	db.SC().SetCheckpointScheduler(controller.NewCheckpointScheduler(config.CheckpointConfig{BlockInterval: 1}))
	for block := first; block <= last; block++ {
		require.NoError(t, db.CommitStateChanges(block, changeset("key", fmt.Sprintf("value-%d", block))))
	}
	require.NoError(t, db.SC().FlushSnapshots())
	require.NoError(t, db.SC().FlushHashes())
}

// recordedHashes returns the vault's hashes for blocks first through last, failing the test if any is
// not recorded.
func recordedHashes(t *testing.T, db *StateDB, first uint64, last uint64) map[uint64][32]byte {
	t.Helper()
	hashes := make(map[uint64][32]byte)
	for block := first; block <= last; block++ {
		hash, status, err := db.GetBlockHash(block)
		require.NoError(t, err)
		require.Equal(t, gigatypes.BlockHashStatusFound, status, "block %d", block)
		hashes[block] = hash
	}
	return hashes
}

// requireStatus asserts the status GetBlockHash reports for blockNumber.
func requireStatus(t *testing.T, db *StateDB, blockNumber uint64, want gigatypes.BlockHashStatus) {
	t.Helper()
	_, status, err := db.GetBlockHash(blockNumber)
	require.NoError(t, err)
	require.Equal(t, want, status, "block %d", blockNumber)
}

// Every block committed has its hash recorded, and the hash recorded is the one SC hands its listeners.
func TestStateDBRecordsTheHashOfEveryCommittedBlock(t *testing.T) {
	stores := newVaultTestStores(t)
	db := stores.open(t)
	defer func() { require.NoError(t, db.Close()) }()

	var mu sync.Mutex
	dispatched := make(map[uint64][32]byte)
	_, err := db.RegisterHashListener(func(_ context.Context, blockNumber uint64, hash *lthash.BlockHash) error {
		mu.Lock()
		defer mu.Unlock()
		dispatched[blockNumber] = hash.Global.Checksum()
		return nil
	})
	require.NoError(t, err)

	commitBlocks(t, db, 1, 5)

	require.Equal(t, uint64(5), db.GetBlockHeight())
	recorded := recordedHashes(t, db, 1, 5)
	mu.Lock()
	require.Equal(t, dispatched, recorded)
	mu.Unlock()
	requireStatus(t, db, 6, gigatypes.BlockHashStatusNotReady)
}

// A reopened StateDB holds the hash of the block it opened on, whether or not anything was replayed.
func TestAReopenedStateDBHoldsTheLoadedBlocksHash(t *testing.T) {
	stores := newVaultTestStores(t)
	db := stores.open(t)
	commitBlocks(t, db, 1, 5)
	before := recordedHashes(t, db, 1, 5)
	require.NoError(t, db.Close())

	reopened := stores.open(t)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.Equal(t, uint64(5), reopened.GetBlockHeight())
	require.Equal(t, before, recordedHashes(t, reopened, 1, 5))
}

// A vault that lost its tail is behind SC, and the blocks it lost can only be hashed again by replaying
// them, so the open rewinds SC to the vault's newest block and replays from there.
func TestAVaultBehindSCIsRefilledByReplay(t *testing.T) {
	stores := newVaultTestStores(t)
	db := stores.open(t)
	commitBlocks(t, db, 1, 6)
	before := recordedHashes(t, db, 1, 6)
	require.NoError(t, db.Close())

	require.NoError(t, hashvault.HardRollbackPebbleHashVault(context.Background(), stores.hashVaultCfg, 3))

	reopened := stores.open(t)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.Equal(t, uint64(6), reopened.GetBlockHeight())
	require.Equal(t, before, recordedHashes(t, reopened, 1, 6))
}

// An empty vault is refilled by rewinding SC the configured number of blocks and replaying them, so it
// holds the hashes of the newest blocks rather than just the loaded one.
func TestAnEmptyVaultIsRefilledByRewinding(t *testing.T) {
	stores := newVaultTestStores(t)
	db := stores.open(t)
	commitBlocks(t, db, 1, 8)
	before := recordedHashes(t, db, 1, 8)
	require.NoError(t, db.Close())

	require.NoError(t, os.RemoveAll(stores.hashVaultCfg.DataDir))
	stores.hashVaultCfg.EmptyVaultRollbackBlocks = 3

	reopened := stores.open(t)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.Equal(t, uint64(8), reopened.GetBlockHeight())
	require.Equal(t, map[uint64][32]byte{6: before[6], 7: before[7], 8: before[8]},
		recordedHashes(t, reopened, 6, 8))
	requireStatus(t, reopened, 5, gigatypes.BlockHashStatusTooOld)
}

// A rewind deeper than SC's snapshots and the WAL can replay is shortened to the deepest one they can.
func TestAnEmptyVaultRewindIsShortenedToWhatIsReachable(t *testing.T) {
	stores := newVaultTestStores(t)
	db := stores.open(t)
	commitBlocks(t, db, 1, 8)
	before := recordedHashes(t, db, 1, 8)
	require.NoError(t, db.Close())

	require.NoError(t, os.RemoveAll(stores.hashVaultCfg.DataDir))
	stores.hashVaultCfg.EmptyVaultRollbackBlocks = 1000

	reopened := stores.open(t)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.Equal(t, uint64(8), reopened.GetBlockHeight())
	recorded := recordedHashes(t, reopened, 2, 8)
	for block, hash := range recorded {
		require.Equal(t, before[block], hash, "block %d", block)
	}
}

// With no rewind configured, an empty vault records only the loaded block's hash.
func TestAnEmptyVaultWithNoRewindRecordsOnlyTheLoadedBlock(t *testing.T) {
	stores := newVaultTestStores(t)
	db := stores.open(t)
	commitBlocks(t, db, 1, 4)
	before := recordedHashes(t, db, 1, 4)
	require.NoError(t, db.Close())

	require.NoError(t, os.RemoveAll(stores.hashVaultCfg.DataDir))
	stores.hashVaultCfg.EmptyVaultRollbackBlocks = 0

	reopened := stores.open(t)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.Equal(t, map[uint64][32]byte{4: before[4]}, recordedHashes(t, reopened, 4, 4))
	requireStatus(t, reopened, 3, gigatypes.BlockHashStatusTooOld)
}

// tamperLoadedBlock replaces the vault's hash for blockNumber with one SC will never produce.
func tamperLoadedBlock(t *testing.T, stores *vaultTestStores, blockNumber uint64) {
	t.Helper()
	cfg := stores.hashVaultCfg
	cfg.HaltOnMismatch = false
	vault, err := hashvault.NewUnsafePebbleHashVault(context.Background(), cfg)
	require.NoError(t, err)
	tampered := [32]byte{0xEE}
	require.NoError(t, vault.CommitToHash(context.Background(), blockNumber, tampered[:]))
	require.NoError(t, vault.Close(context.Background()))
}

// The loaded block's hash is checked against the vault even when nothing replays, so a vault that
// disagrees with the state on disk stops the open when halting is selected.
func TestAMismatchAtTheLoadedBlockFailsTheOpenWhenHalting(t *testing.T) {
	stores := newVaultTestStores(t)
	db := stores.open(t)
	commitBlocks(t, db, 1, 3)
	require.NoError(t, db.Close())

	tamperLoadedBlock(t, stores, 3)

	_, err := stores.openErr()
	require.ErrorContains(t, err, "mismatch")
}

// With halting off, the same disagreement is logged and the state's own hash replaces the vault's.
func TestAMismatchAtTheLoadedBlockIsReplacedWhenNotHalting(t *testing.T) {
	stores := newVaultTestStores(t)
	db := stores.open(t)
	commitBlocks(t, db, 1, 3)
	before := recordedHashes(t, db, 1, 3)
	require.NoError(t, db.Close())

	tamperLoadedBlock(t, stores, 3)
	stores.hashVaultCfg.HaltOnMismatch = false

	reopened := stores.open(t)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.Equal(t, before, recordedHashes(t, reopened, 1, 3))
}

// A rollback leaves the vault alone: the blocks above the target are re-executed, and it is exactly their
// recorded hashes the re-execution is held to.
func TestARollbackKeepsTheHashesAboveItsTarget(t *testing.T) {
	stores := newVaultTestStores(t)
	db := stores.open(t)
	commitBlocks(t, db, 1, 5)
	before := recordedHashes(t, db, 1, 5)
	require.NoError(t, db.Close())

	rolledBack, err := NewStateDB(context.Background(),
		stores.flatkvCfg, stores.ssCfg, stores.checkpointCfg, stores.hashVaultCfg, 3)
	require.NoError(t, err)
	defer func() { require.NoError(t, rolledBack.Close()) }()

	require.Equal(t, uint64(3), rolledBack.GetBlockHeight())
	require.Equal(t, before, recordedHashes(t, rolledBack, 1, 5))

	commitBlocks(t, rolledBack, 4, 5)
	require.Equal(t, before, recordedHashes(t, rolledBack, 1, 5), "re-executing the same blocks matches")
}

// Re-executing a block into a different state is the equivocation the vault exists to stop. The vault is
// SC's first listener, so a hash it refuses reaches no listener registered after it.
func TestADifferentReexecutionIsRefusedBeforeAnyOtherListenerSeesIt(t *testing.T) {
	stores := newVaultTestStores(t)
	db := stores.open(t)
	commitBlocks(t, db, 1, 5)
	require.NoError(t, db.Close())

	rolledBack, err := NewStateDB(context.Background(),
		stores.flatkvCfg, stores.ssCfg, stores.checkpointCfg, stores.hashVaultCfg, 3)
	require.NoError(t, err)
	defer func() { _ = rolledBack.Close() }()

	var mu sync.Mutex
	var seen []uint64
	record := func(_ context.Context, blockNumber uint64, _ *lthash.BlockHash) error {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, blockNumber)
		return nil
	}
	_, err = rolledBack.RegisterHashListener(record)
	require.NoError(t, err)

	require.NoError(t, rolledBack.CommitStateChanges(4, changeset("key", "a different value")))
	require.ErrorContains(t, rolledBack.SC().FlushHashes(), "mismatch")
	mu.Lock()
	require.Empty(t, seen, "a hash the vault refused must reach no later listener")
	mu.Unlock()
}

// The vault joins the prune cycle, which is how the storage garbage collector's permission reaches it.
func TestTheVaultJoinsThePruneCycle(t *testing.T) {
	stores := newVaultTestStores(t)
	db := stores.open(t)
	defer func() { require.NoError(t, db.Close()) }()

	var names []string
	for _, store := range db.PrunableStores() {
		names = append(names, store.Name())
	}
	require.Contains(t, names, "HashVault")
}
