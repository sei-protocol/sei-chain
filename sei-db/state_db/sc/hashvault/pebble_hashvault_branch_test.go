package hashvault

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// commitHeights commits the hashes of heights first to last, each seeded with its own height.
func commitHeights(t *testing.T, v *PebbleHashVault, first uint64, last uint64) {
	t.Helper()
	for h := first; h <= last; h++ {
		require.NoError(t, v.CommitToHash(context.Background(), h, bytesOfLen(byte(h), 32)))
	}
}

// requireStatus asserts the status Get reports for height.
func requireStatus(t *testing.T, v *PebbleHashVault, height uint64, want gigatypes.BlockHashStatus) {
	t.Helper()
	_, status, err := v.Get(height)
	require.NoError(t, err)
	require.Equal(t, want, status, "height %d", height)
}

// requireRecorded asserts the vault holds a hash seeded with seed for height.
func requireRecorded(t *testing.T, v *PebbleHashVault, height uint64, seed byte) {
	t.Helper()
	hash, status, err := v.Get(height)
	require.NoError(t, err)
	require.Equal(t, gigatypes.BlockHashStatusFound, status, "height %d", height)
	require.Equal(t, bytesOfLen(seed, 32), hash[:], "height %d", height)
}

func warnOnMismatch(cfg *HashVaultConfig) {
	cfg.HaltOnMismatch = false
}

func TestCommitRefusesAGap(t *testing.T) {
	v := newTestPebbleVault(t)
	commitHeights(t, v, 1, 3)
	require.ErrorContains(t, v.CommitToHash(context.Background(), 5, bytesOfLen(5, 32)), "gap")
	head, recorded := v.Head()
	require.True(t, recorded)
	require.Equal(t, uint64(3), head)
}

func TestHeadSurvivesARestart(t *testing.T) {
	v := newTestPebbleVault(t)
	commitHeights(t, v, 1, 3)
	v2 := reopenTestPebbleVault(t, v)
	head, recorded := v2.Head()
	require.True(t, recorded)
	require.Equal(t, uint64(3), head)
	require.ErrorContains(t, v2.CommitToHash(context.Background(), 5, bytesOfLen(5, 32)), "gap")
}

func TestMismatchReplacesTheRecordWhenNotHalting(t *testing.T) {
	v := newTestPebbleVault(t, warnOnMismatch)
	commitHeights(t, v, 1, 5)

	require.NoError(t, v.CommitToHash(context.Background(), 3, bytesOfLen(0xEE, 32)))
	requireRecorded(t, v, 2, 2)
	requireRecorded(t, v, 3, 0xEE)
	requireStatus(t, v, 4, gigatypes.BlockHashStatusNotReady)
	require.NoError(t, v.CommitToHash(context.Background(), 4, bytesOfLen(0xEF, 32)))
}

func TestCommitBelowTheOldestRecordedHeight(t *testing.T) {
	t.Run("halting", func(t *testing.T) {
		v := newTestPebbleVault(t)
		commitHeights(t, v, 10, 12)
		require.ErrorIs(t, v.CommitToHash(context.Background(), 5, bytesOfLen(5, 32)), ErrBelowPruneBoundary)
	})
	t.Run("not halting", func(t *testing.T) {
		v := newTestPebbleVault(t, warnOnMismatch)
		commitHeights(t, v, 10, 12)
		require.NoError(t, v.CommitToHash(context.Background(), 5, bytesOfLen(5, 32)))
		requireRecorded(t, v, 5, 5)
		requireStatus(t, v, 10, gigatypes.BlockHashStatusNotReady)
	})
}

func TestGetStatuses(t *testing.T) {
	v := newTestPebbleVault(t)
	requireStatus(t, v, 1, gigatypes.BlockHashStatusNotReady)

	commitHeights(t, v, 1, 10)
	require.NoError(t, v.Prune(context.Background(), 5))
	requireStatus(t, v, 4, gigatypes.BlockHashStatusTooOld)
	requireRecorded(t, v, 5, 5)
	requireStatus(t, v, 11, gigatypes.BlockHashStatusNotReady)

	require.NoError(t, v.Close(context.Background()))
	_, status, err := v.Get(5)
	require.ErrorIs(t, err, ErrClosed)
	require.Equal(t, gigatypes.BlockHashStatusError, status)
}

func TestResetLeavesOnlyTheGivenHeight(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)
	commitHeights(t, v, 1, 5)
	require.NoError(t, v.Prune(ctx, 3))

	require.NoError(t, v.Reset(ctx, 1000, bytesOfLen(0xAB, 32)))
	requireRecorded(t, v, 1000, 0xAB)
	requireStatus(t, v, 4, gigatypes.BlockHashStatusTooOld)
	require.NoError(t, v.CommitToHash(ctx, 1001, bytesOfLen(0xAC, 32)))

	cfg := v.config
	require.NoError(t, v.Close(ctx))
	oldest, newest, recorded, err := StoredRange(cfg)
	require.NoError(t, err)
	require.True(t, recorded)
	require.Equal(t, uint64(1000), oldest)
	require.Equal(t, uint64(1001), newest)
}

func TestPruneHistoryDeletesOnlyBelowBothFloors(t *testing.T) {
	v := newTestPebbleVault(t)
	commitHeights(t, v, 1, 150)

	require.NoError(t, v.PruneHistory(60))
	requireRecorded(t, v, 1, 1)

	v.PruneBelow(100)
	require.NoError(t, v.PruneHistory(60))
	requireStatus(t, v, 59, gigatypes.BlockHashStatusTooOld)
	requireRecorded(t, v, 60, 60)

	require.NoError(t, v.PruneHistory(200))
	requireStatus(t, v, 99, gigatypes.BlockHashStatusTooOld)
	requireRecorded(t, v, 100, 100)

	v.PruneBelow(1000)
	require.NoError(t, v.PruneHistory(1000))
	requireStatus(t, v, 149, gigatypes.BlockHashStatusTooOld)
	requireRecorded(t, v, 150, 150)
}

func TestRollbackFloorIsTheHeadLessTheWindow(t *testing.T) {
	v := newTestPebbleVault(t)
	require.Equal(t, uint64(0), v.GetRollbackFloor(10))
	commitHeights(t, v, 1, 30)
	require.Equal(t, uint64(20), v.GetRollbackFloor(10))
	require.Equal(t, uint64(0), v.GetRollbackFloor(40))
	latest, err := v.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(30), latest)
}

func TestStoredRange(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultHashVaultConfig()
	cfg.DataDir = filepath.Join(t.TempDir(), "vault")

	_, _, recorded, err := StoredRange(cfg)
	require.NoError(t, err)
	require.False(t, recorded, "a vault that was never created records nothing")

	v, err := NewUnsafePebbleHashVault(ctx, cfg)
	require.NoError(t, err)
	require.NoError(t, v.Close(ctx))
	_, _, recorded, err = StoredRange(cfg)
	require.NoError(t, err)
	require.False(t, recorded, "an empty vault records nothing")

	v, err = NewUnsafePebbleHashVault(ctx, cfg)
	require.NoError(t, err)
	commitHeights(t, v, 4, 9)
	require.NoError(t, v.Close(ctx))
	oldest, newest, recorded, err := StoredRange(cfg)
	require.NoError(t, err)
	require.True(t, recorded)
	require.Equal(t, uint64(4), oldest)
	require.Equal(t, uint64(9), newest)
}

func TestOpenDeletesTheLegacyVault(t *testing.T) {
	legacy := filepath.Join(t.TempDir(), "hashvault")
	require.NoError(t, os.MkdirAll(legacy, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(legacy, "000001.log"), []byte("x"), 0o600))

	newTestPebbleVault(t, func(cfg *HashVaultConfig) { cfg.LegacyPebbleDir = legacy })
	_, err := os.Stat(legacy)
	require.True(t, os.IsNotExist(err), "the legacy vault must be gone, got %v", err)

	newTestPebbleVault(t, func(cfg *HashVaultConfig) { cfg.LegacyPebbleDir = legacy })
}
