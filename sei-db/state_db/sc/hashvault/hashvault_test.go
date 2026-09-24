package hashvault

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// testConfig returns a config for a vault in a fresh directory. Fsync is off, since the tests flush after
// every hash and the durability is LittDB's to prove, not this package's.
func testConfig(t *testing.T, haltOnMismatch bool) config.HashVaultConfig {
	t.Helper()
	cfg := config.DefaultHashVaultConfig()
	cfg.DataDir = filepath.Join(t.TempDir(), "hashvault")
	cfg.HaltOnMismatch = haltOnMismatch
	cfg.Fsync = false
	return cfg
}

// openVault opens a vault from cfg, closed when the test ends.
func openVault(t *testing.T, cfg config.HashVaultConfig) *HashVault {
	t.Helper()
	v, err := Open(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, v.Close()) })
	return v
}

// hashOf returns a hash that differs for every distinct seed.
func hashOf(seed byte) [32]byte {
	var hash [32]byte
	for i := range hash {
		hash[i] = seed
	}
	return hash
}

// commitRange commits the hashes of blocks first to last, each seeded with its own block number.
func commitRange(t *testing.T, v *HashVault, first uint64, last uint64) {
	t.Helper()
	for block := first; block <= last; block++ {
		require.NoError(t, v.Commit(block, hashOf(byte(block))))
	}
}

// requireHash asserts the vault holds want for blockNumber.
func requireHash(t *testing.T, v *HashVault, blockNumber uint64, want [32]byte) {
	t.Helper()
	got, status, err := v.Get(blockNumber)
	require.NoError(t, err)
	require.Equal(t, gigatypes.BlockHashStatusFound, status, "block %d", blockNumber)
	require.Equal(t, want, got, "block %d", blockNumber)
}

// An empty vault has no range to hold a block to, so the first block may be any height, and nothing is
// ready to be read until it is recorded.
func TestAnEmptyVaultTakesAnyFirstBlock(t *testing.T) {
	v := openVault(t, testConfig(t, true))

	_, recorded := v.Head()
	require.False(t, recorded)
	_, status, err := v.Get(5)
	require.NoError(t, err)
	require.Equal(t, gigatypes.BlockHashStatusNotReady, status)

	require.NoError(t, v.Commit(100, hashOf(1)))
	head, recorded := v.Head()
	require.True(t, recorded)
	require.Equal(t, uint64(100), head)
	requireHash(t, v, 100, hashOf(1))
}

// Blocks above the newest recorded one are not ready, whether or not they have been committed yet.
func TestABlockAboveTheHeadIsNotReady(t *testing.T) {
	v := openVault(t, testConfig(t, true))
	commitRange(t, v, 1, 3)

	_, status, err := v.Get(4)
	require.NoError(t, err)
	require.Equal(t, gigatypes.BlockHashStatusNotReady, status)
}

// The recorded range is contiguous, so a block that would leave a gap is refused whatever the mismatch
// policy is.
func TestAGapIsRefused(t *testing.T) {
	for _, halt := range []bool{true, false} {
		v := openVault(t, testConfig(t, halt))
		commitRange(t, v, 1, 3)

		require.ErrorContains(t, v.Commit(5, hashOf(5)), "gap")
		head, _ := v.Head()
		require.Equal(t, uint64(3), head, "a refused block must not be recorded")
	}
}

// Re-execution reproduces the hashes it recorded before, so committing the same hash again is a check
// that passes, not a write.
func TestRecommittingTheSameHashPasses(t *testing.T) {
	v := openVault(t, testConfig(t, true))
	commitRange(t, v, 1, 5)

	commitRange(t, v, 2, 5)
	head, _ := v.Head()
	require.Equal(t, uint64(5), head)
	requireHash(t, v, 3, hashOf(3))
}

// With halting selected, a different hash for a recorded block fails and leaves the record as it was.
func TestAMismatchHaltsWhenHaltingIsSelected(t *testing.T) {
	v := openVault(t, testConfig(t, true))
	commitRange(t, v, 1, 5)

	require.ErrorContains(t, v.Commit(3, hashOf(0xEE)), "mismatch")
	requireHash(t, v, 3, hashOf(3))
	head, _ := v.Head()
	require.Equal(t, uint64(5), head)
}

// With halting off, a different hash replaces the recorded one, and the hashes above it go with it: they
// were derived from the state the new hash disowns.
func TestAMismatchReplacesTheRecordWhenHaltingIsOff(t *testing.T) {
	v := openVault(t, testConfig(t, false))
	commitRange(t, v, 1, 5)

	require.NoError(t, v.Commit(3, hashOf(0xEE)))
	requireHash(t, v, 2, hashOf(2))
	requireHash(t, v, 3, hashOf(0xEE))
	head, _ := v.Head()
	require.Equal(t, uint64(3), head)
	_, status, err := v.Get(4)
	require.NoError(t, err)
	require.Equal(t, gigatypes.BlockHashStatusNotReady, status)

	require.NoError(t, v.Commit(4, hashOf(0xEF)), "commits carry on from the replaced block")
	requireHash(t, v, 4, hashOf(0xEF))
}

// A mismatch at the oldest recorded block discards every hash, which the vault survives as an empty one.
func TestAMismatchAtTheOldestBlockLeavesOnlyTheNewHash(t *testing.T) {
	v := openVault(t, testConfig(t, false))
	commitRange(t, v, 10, 12)

	require.NoError(t, v.Commit(10, hashOf(0xEE)))
	requireHash(t, v, 10, hashOf(0xEE))
	head, _ := v.Head()
	require.Equal(t, uint64(10), head)
}

// What the vault records survives a restart, including a record a mismatch rewrote.
func TestTheRecordSurvivesAReopen(t *testing.T) {
	cfg := testConfig(t, false)
	v, err := Open(cfg)
	require.NoError(t, err)
	commitRange(t, v, 1, 5)
	require.NoError(t, v.Commit(4, hashOf(0xEE)))
	require.NoError(t, v.Close())

	reopened := openVault(t, cfg)
	head, recorded := reopened.Head()
	require.True(t, recorded)
	require.Equal(t, uint64(4), head)
	requireHash(t, reopened, 3, hashOf(3))
	requireHash(t, reopened, 4, hashOf(0xEE))
	require.ErrorContains(t, reopened.Commit(6, hashOf(6)), "gap", "the reopened vault still refuses gaps")
}

// Reset leaves the block it is given as the only one recorded, and commits carry on from it.
func TestResetLeavesOnlyTheGivenBlock(t *testing.T) {
	v := openVault(t, testConfig(t, true))
	commitRange(t, v, 1, 5)

	require.NoError(t, v.Reset(1000, hashOf(0xAB)))
	head, recorded := v.Head()
	require.True(t, recorded)
	require.Equal(t, uint64(1000), head)
	requireHash(t, v, 1000, hashOf(0xAB))
	require.Equal(t, uint64(1), v.table.KeyCount())

	require.NoError(t, v.Commit(1001, hashOf(0xAC)))
	requireHash(t, v, 1001, hashOf(0xAC))
}

// The Pebble vault this one replaced holds app hashes nothing can use, so opening deletes it.
func TestOpenDeletesTheLegacyPebbleVault(t *testing.T) {
	cfg := testConfig(t, true)
	cfg.LegacyPebbleDir = filepath.Join(t.TempDir(), "hashvault")
	require.NoError(t, os.MkdirAll(cfg.LegacyPebbleDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(cfg.LegacyPebbleDir, "000001.log"), []byte("x"), 0o600))

	openVault(t, cfg)
	_, err := os.Stat(cfg.LegacyPebbleDir)
	require.True(t, os.IsNotExist(err), "the legacy vault must be gone, got %v", err)
}

// A legacy dir that is not there is the common case once the testnet has run this build, not an error.
func TestOpenWithoutALegacyPebbleVault(t *testing.T) {
	cfg := testConfig(t, true)
	cfg.LegacyPebbleDir = filepath.Join(t.TempDir(), "absent")
	openVault(t, cfg)
}

// A hash may be deleted only once both the owner and the storage garbage collector permit it, and neither
// permission is taken back by a later, lower one.
func TestGCDeletesOnlyBelowBothFloors(t *testing.T) {
	v := openVault(t, testConfig(t, true))
	deletable := func(blockNumber uint64) bool {
		t.Helper()
		ok, err := v.gcFilter(encodeKey(blockNumber), true)
		require.NoError(t, err)
		return ok
	}

	require.False(t, deletable(0), "nothing is deletable before either floor is raised")

	v.PruneBelow(100)
	require.False(t, deletable(50), "the owner alone cannot delete a hash")

	require.NoError(t, v.PruneHistory(60))
	require.True(t, deletable(59))
	require.False(t, deletable(60), "the lower floor bounds what is deleted")
	require.False(t, deletable(99))

	require.NoError(t, v.PruneHistory(200))
	require.True(t, deletable(99))
	require.False(t, deletable(100), "the owner's floor now bounds what is deleted")

	v.PruneBelow(10)
	require.NoError(t, v.PruneHistory(10))
	require.True(t, deletable(99), "a lower floor must not take back a permission already given")
}

// The vault restores nothing from snapshots, so its rollback floor is its newest block less the window.
func TestRollbackFloorIsTheHeadLessTheWindow(t *testing.T) {
	v := openVault(t, testConfig(t, true))
	require.Equal(t, uint64(0), v.GetRollbackFloor(10), "an empty vault constrains nothing")

	commitRange(t, v, 1, 30)
	require.Equal(t, uint64(20), v.GetRollbackFloor(10))
	require.Equal(t, uint64(0), v.GetRollbackFloor(40), "a window deeper than the history floors at 0")
	latest, err := v.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(30), latest)
}

// A record written in a format this build does not know is refused rather than read as a hash.
func TestAnUnknownRecordFormatIsRefused(t *testing.T) {
	value := encodeValue(hashOf(1))
	value[0] = recordFormatVersion + 1
	_, err := decodeValue(value)
	require.ErrorContains(t, err, "format version")

	_, err = decodeValue(value[:10])
	require.ErrorContains(t, err, "bytes")
}

// Every method fails once the vault is closed, rather than reading a table that is gone.
func TestAClosedVaultRefusesEverything(t *testing.T) {
	v, err := Open(testConfig(t, true))
	require.NoError(t, err)
	commitRange(t, v, 1, 2)
	require.NoError(t, v.Close())
	require.NoError(t, v.Close(), "closing twice is harmless")

	require.Error(t, v.Commit(3, hashOf(3)))
	require.Error(t, v.Reset(3, hashOf(3)))
	_, status, err := v.Get(1)
	require.Error(t, err)
	require.Equal(t, gigatypes.BlockHashStatusError, status)
}
