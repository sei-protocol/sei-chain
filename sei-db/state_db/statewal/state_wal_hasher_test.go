package statewal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// newTestHasher returns a hasher stopped when the test ends, and the context it bricks.
func newTestHasher(
	t *testing.T,
	// Whether to seed the hasher with a last stored block.
	hasLast bool,
	// The seeded block. Ignored unless hasLast.
	lastBlockNumber uint64,
) (*stateWALHasher, context.Context) {
	ctx, cancel := context.WithCancelCause(context.Background())
	h := newStateWALHasher(ctx, cancel, 4, hasLast, lastBlockNumber, [32]byte{})
	t.Cleanup(func() {
		cancel(nil)
		h.AwaitExit()
	})
	return h, ctx
}

// A hash job that skips past the seeded block bricks the hasher.
func TestHasherBricksOnGapAfterSeed(t *testing.T) {
	h, ctx := newTestHasher(t, true, 5)

	require.True(t, h.ScheduleHash(7, nil))
	require.False(t, h.Flush())
	require.ErrorContains(t, context.Cause(ctx), "block 7")
}

// The first hash job of an unseeded hasher may be any block, and a repeated block bricks it.
func TestHasherBricksOnRepeatedBlock(t *testing.T) {
	h, ctx := newTestHasher(t, false, 0)

	require.True(t, h.ScheduleHash(3, nil))
	require.True(t, h.ScheduleHash(4, nil))
	require.True(t, h.Flush())

	require.True(t, h.ScheduleHash(4, nil))
	require.False(t, h.Flush())
	require.ErrorContains(t, context.Cause(ctx), "block 4")
}
