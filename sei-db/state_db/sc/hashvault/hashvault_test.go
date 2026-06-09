package hashvault

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Contract-level tests for HashVault. These exercise the externally-visible behavior promised by
// the HashVault interface against the PebbleHashVault implementation. Pebble-specific surface
// (encoding, restart recovery, on-disk inspection, the static rollback function, etc.) is tested
// per-implementation in pebble_hashvault_test.go and pebble_hashvault_rollback_test.go.

func bytesOfLen(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestCommitRejectsInvalidHashLength(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)

	require.ErrorIs(t, v.CommitToHash(ctx, 1, nil), ErrInvalidHashLength)
	require.ErrorIs(t, v.CommitToHash(ctx, 1, []byte{}), ErrInvalidHashLength)
	require.ErrorIs(t, v.CommitToHash(ctx, 1, bytesOfLen(0xAA, 31)), ErrInvalidHashLength)
	require.ErrorIs(t, v.CommitToHash(ctx, 1, bytesOfLen(0xAA, 33)), ErrInvalidHashLength)
}

func TestCommitFirstTime(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)
	hash := bytesOfLen(0xAA, 32)
	require.NoError(t, v.CommitToHash(ctx, 7, hash))
}

func TestCommitIdempotent(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)
	hash := bytesOfLen(0xAB, 32)
	require.NoError(t, v.CommitToHash(ctx, 7, hash))
	require.NoError(t, v.CommitToHash(ctx, 7, hash))
	require.NoError(t, v.CommitToHash(ctx, 7, hash))
}

func TestCommitMismatch(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)
	a := bytesOfLen(0x01, 32)
	b := bytesOfLen(0x02, 32)
	require.NoError(t, v.CommitToHash(ctx, 42, a))

	err := v.CommitToHash(ctx, 42, b)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrHashMismatch)
}

func TestCommitMismatchAfterRepeatedCommitIsSticky(t *testing.T) {
	// Even after re-committing the same hash many times, a single mismatch still surfaces. This
	// is essentially a regression check that the cache fast path also enforces the mismatch.
	ctx := context.Background()
	v := newTestPebbleVault(t)
	a := bytesOfLen(0x55, 32)
	b := bytesOfLen(0x66, 32)
	for i := 0; i < 10; i++ {
		require.NoError(t, v.CommitToHash(ctx, 5, a))
	}
	err := v.CommitToHash(ctx, 5, b)
	require.ErrorIs(t, err, ErrHashMismatch)
}

func TestPruneRemovesData(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)

	// Commit a handful of heights, prune below 5, then probe around the boundary.
	for h := uint64(1); h <= 10; h++ {
		require.NoError(t, v.CommitToHash(ctx, h, bytesOfLen(byte(h), 32)))
	}
	require.NoError(t, v.Prune(ctx, 5))

	// Below the boundary is rejected.
	require.ErrorIs(t,
		v.CommitToHash(ctx, 3, bytesOfLen(0x03, 32)),
		ErrBelowPruneBoundary,
	)
	// At the boundary is allowed (and the previously-committed hash is still locked in).
	require.NoError(t, v.CommitToHash(ctx, 5, bytesOfLen(0x05, 32)))
	require.ErrorIs(t,
		v.CommitToHash(ctx, 5, bytesOfLen(0x55, 32)),
		ErrHashMismatch,
	)
	// Above the boundary is allowed and still locked.
	require.NoError(t, v.CommitToHash(ctx, 7, bytesOfLen(0x07, 32)))
	require.ErrorIs(t,
		v.CommitToHash(ctx, 7, bytesOfLen(0x77, 32)),
		ErrHashMismatch,
	)
}

func TestCommitBelowPruneBoundary(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)

	require.NoError(t, v.Prune(ctx, 100))
	// Strictly below the boundary is rejected.
	require.ErrorIs(t,
		v.CommitToHash(ctx, 99, bytesOfLen(0xAA, 32)),
		ErrBelowPruneBoundary,
	)
	require.ErrorIs(t,
		v.CommitToHash(ctx, 50, bytesOfLen(0xAA, 32)),
		ErrBelowPruneBoundary,
	)
	// At the boundary is allowed: Prune keeps the boundary block per the godoc.
	require.NoError(t, v.CommitToHash(ctx, 100, bytesOfLen(0xAA, 32)))
	// Above is also obviously fine.
	require.NoError(t, v.CommitToHash(ctx, 101, bytesOfLen(0xAA, 32)))
}

func TestPruneMonotonic(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)

	require.NoError(t, v.Prune(ctx, 50))
	require.NoError(t, v.Prune(ctx, 25)) // no-op
	// Committing at 30 still errors: the effective boundary is still 50.
	require.ErrorIs(t,
		v.CommitToHash(ctx, 30, bytesOfLen(0xAA, 32)),
		ErrBelowPruneBoundary,
	)
	// Just below the boundary still errors.
	require.ErrorIs(t,
		v.CommitToHash(ctx, 49, bytesOfLen(0xAA, 32)),
		ErrBelowPruneBoundary,
	)
	// At and above the boundary succeed.
	require.NoError(t, v.CommitToHash(ctx, 50, bytesOfLen(0x50, 32)))
	require.NoError(t, v.CommitToHash(ctx, 51, bytesOfLen(0xAA, 32)))
}

func TestCloseIsIdempotent(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)
	require.NoError(t, v.Close(ctx))
	require.NoError(t, v.Close(ctx))
	require.NoError(t, v.Close(ctx))
}

func TestCallsAfterCloseError(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)
	require.NoError(t, v.Close(ctx))
	require.ErrorIs(t, v.CommitToHash(ctx, 1, bytesOfLen(0xAA, 32)), ErrClosed)
	require.ErrorIs(t, v.Prune(ctx, 1), ErrClosed)
}

func TestConcurrentCommits(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)

	// 100 goroutines, each commits a distinct height. All should succeed.
	var wg sync.WaitGroup
	const N = 100
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(h uint64) {
			defer wg.Done()
			errs <- v.CommitToHash(ctx, h, bytesOfLen(byte(h), 32))
		}(uint64(i + 1))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	// Re-committing the same (height, hash) from many goroutines should also all succeed.
	errs2 := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(h uint64) {
			defer wg.Done()
			errs2 <- v.CommitToHash(ctx, h, bytesOfLen(byte(h), 32))
		}(uint64(i + 1))
	}
	wg.Wait()
	close(errs2)
	for err := range errs2 {
		require.NoError(t, err)
	}

	// Committing a *different* hash at any of those heights from many goroutines should yield
	// at least one mismatch error and never a hidden success.
	errs3 := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(h uint64) {
			defer wg.Done()
			errs3 <- v.CommitToHash(ctx, h, bytesOfLen(0xFF, 32))
		}(uint64(i + 1))
	}
	wg.Wait()
	close(errs3)
	mismatches := 0
	for err := range errs3 {
		require.Error(t, err)
		if errors.Is(err, ErrHashMismatch) {
			mismatches++
		}
	}
	require.Equal(t, N, mismatches, "every concurrent different-hash commit must return ErrHashMismatch")
}

// Sanity check that fmt.Errorf wrapping of our sentinels via %w stays Is-compatible. Defends
// against accidental future refactors of the codec or handlers that lose the sentinel.
func TestErrorWrappingIsCompatible(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", ErrCorruption)
	require.ErrorIs(t, wrapped, ErrCorruption)
	wrappedLen := fmt.Errorf("outer: %w", ErrInvalidHashLength)
	require.ErrorIs(t, wrappedLen, ErrInvalidHashLength)
}
