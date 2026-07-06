package hashvault

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
)

// TestHardRollbackPebbleHashVault covers the happy path: an existing commit at height 10 is
// removed by rolling back to height 5; a fresh commit at 10 with a different hash then succeeds
// and is itself locked in. The vault must be closed before invoking the static function (Pebble's
// directory lock would otherwise refuse).
func TestHardRollbackPebbleHashVault(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)

	a := bytesOfLen(0xAA, 32)
	b := bytesOfLen(0xBB, 32)
	require.NoError(t, v.CommitToHash(ctx, 10, a))

	cfg := v.config
	require.NoError(t, v.Close(ctx))

	require.NoError(t, HardRollbackPebbleHashVault(ctx, cfg, 5))

	v2, err := NewUnsafePebbleHashVault(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v2.Close(ctx) })

	require.NoError(t, v2.CommitToHash(ctx, 10, b))
	require.ErrorIs(t, v2.CommitToHash(ctx, 10, a), ErrHashMismatch)
}

func TestHardRollbackPebbleHashVaultBelowPruneBoundaryWipesStore(t *testing.T) {
	// When the rollback target is strictly below the boundary, "partial rollback" is incoherent:
	// every surviving hash has height >= boundary > target, so there is no consistent state to
	// preserve. The function wipes everything (hashes + boundary record) so the next boot looks
	// like a freshly-initialized vault.
	ctx := context.Background()
	v := newTestPebbleVault(t)

	for h := uint64(30); h <= 50; h++ {
		require.NoError(t, v.CommitToHash(ctx, h, bytesOfLen(byte(h), 32)))
	}
	require.NoError(t, v.Prune(ctx, 30))

	cfg := v.config
	require.NoError(t, v.Close(ctx))

	require.NoError(t, HardRollbackPebbleHashVault(ctx, cfg, 10))

	v2, err := NewUnsafePebbleHashVault(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v2.Close(ctx) })

	// Boundary is gone, so commits below the old boundary are now accepted.
	require.NoError(t, v2.CommitToHash(ctx, 5, bytesOfLen(0xCC, 32)))
	// Every previously-locked hash is also gone — height 50 used to be 0x32, but the wipe means a
	// fresh hash there is allowed.
	require.NoError(t, v2.CommitToHash(ctx, 50, bytesOfLen(0xEE, 32)))
}

func TestHardRollbackPebbleHashVaultEqualToPruneBoundary(t *testing.T) {
	// Rollback target == boundary: hashes above the target are removed, the boundary block is kept,
	// and the prune boundary record is cleared so commits below the old boundary are allowed again.
	ctx := context.Background()
	v := newTestPebbleVault(t)
	require.NoError(t, v.Prune(ctx, 100))
	cfg := v.config
	require.NoError(t, v.Close(ctx))

	require.NoError(t, HardRollbackPebbleHashVault(ctx, cfg, 100))

	v2, err := NewUnsafePebbleHashVault(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v2.Close(ctx) })
	require.NoError(t, v2.CommitToHash(ctx, 50, bytesOfLen(0xAA, 32)))
}

func TestHardRollbackPebbleHashVaultRejectsLockedDir(t *testing.T) {
	// Sanity check that the static function fails fast when a live vault still holds the Pebble
	// directory lock — the whole point of being out-of-process is to make accidental concurrent
	// use a clean error, not a silent corruption.
	ctx := context.Background()
	v := newTestPebbleVault(t)
	cfg := v.config

	err := HardRollbackPebbleHashVault(ctx, cfg, 5)
	require.Error(t, err, "must refuse to open while the live vault holds the lock")
}

func TestHardRollbackPebbleHashVaultRefusesMalformedBoundary(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)
	cfg := v.config
	require.NoError(t, v.Close(ctx))

	// Plant a malformed boundary record so the function has to reject before performing any write.
	directWritePebble(t, cfg.DataDir, func(db *pebble.DB) {
		require.NoError(t, db.Set(pruneBoundaryKey, []byte{0x00, 0x01}, pebble.Sync))
	})

	err := HardRollbackPebbleHashVault(ctx, cfg, 100)
	require.ErrorIs(t, err, ErrCorruption)
}

func TestHardRollbackPebbleHashVaultRejectsMaxUint64Height(t *testing.T) {
	// blockHeight+1 must not be used for DeleteRange start keys: at MaxUint64 it wraps to 0 and
	// would wipe the entire vault. Refuse rather than silently destroy data.
	ctx := context.Background()
	v := newTestPebbleVault(t)
	require.NoError(t, v.CommitToHash(ctx, math.MaxUint64, bytesOfLen(0xFF, 32)))

	cfg := v.config
	require.NoError(t, v.Close(ctx))

	err := HardRollbackPebbleHashVault(ctx, cfg, math.MaxUint64)
	require.ErrorIs(t, err, ErrRollbackHeightOverflow)

	v2, err := NewUnsafePebbleHashVault(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v2.Close(ctx) })

	require.ErrorIs(t, v2.CommitToHash(ctx, math.MaxUint64, bytesOfLen(0xEE, 32)), ErrHashMismatch)
}

func TestHardRollbackPebbleHashVaultRejectsMissingDir(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultHashVaultConfig()
	// Point at a path that definitely doesn't exist; pebble.Open is the source of truth for the
	// error here — we just want to verify the static function surfaces it rather than silently
	// creating a fresh empty vault and pretending the rollback succeeded.
	cfg.DataDir = filepath.Join(t.TempDir(), "does-not-exist", "vault")

	err := HardRollbackPebbleHashVault(ctx, cfg, 5)
	require.Error(t, err)
}
