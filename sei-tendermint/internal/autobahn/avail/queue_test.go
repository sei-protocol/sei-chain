package avail

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueueNewEmpty(t *testing.T) {
	q := newQueue[uint64, string]()
	require.Equal(t, uint64(0), q.first)
	require.Equal(t, uint64(0), q.next)
	require.Equal(t, uint64(0), q.Len())
}

func TestQueuePushBack(t *testing.T) {
	q := newQueue[uint64, string]()
	q.pushBack("a")
	q.pushBack("b")
	q.pushBack("c")

	require.Equal(t, uint64(0), q.first)
	require.Equal(t, uint64(3), q.next)
	require.Equal(t, uint64(3), q.Len())
	require.Equal(t, "a", q.q[0])
	require.Equal(t, "b", q.q[1])
	require.Equal(t, "c", q.q[2])
}

func TestQueueReset(t *testing.T) {
	q := newQueue[uint64, string]()
	q.reset(5)

	require.Equal(t, uint64(5), q.first)
	require.Equal(t, uint64(5), q.next)
	require.Equal(t, uint64(0), q.Len())

	q.pushBack("x")
	require.Equal(t, uint64(5), q.first)
	require.Equal(t, uint64(6), q.next)
	require.Equal(t, "x", q.q[5])
}

func TestQueuePrune(t *testing.T) {
	q := newQueue[uint64, string]()
	q.pushBack("a")
	q.pushBack("b")
	q.pushBack("c")
	q.pushBack("d")

	q.prune(2)
	require.Equal(t, uint64(2), q.first)
	require.Equal(t, uint64(4), q.next)
	require.Equal(t, uint64(2), q.Len())

	_, ok := q.q[0]
	require.False(t, ok)
	_, ok = q.q[1]
	require.False(t, ok)
	require.Equal(t, "c", q.q[2])
	require.Equal(t, "d", q.q[3])
}

func TestQueuePruneStale(t *testing.T) {
	q := newQueue[uint64, string]()
	q.pushBack("a")
	q.pushBack("b")

	q.prune(1)
	q.prune(0) // stale, should be no-op

	require.Equal(t, uint64(1), q.first)
	require.Equal(t, uint64(2), q.next)
	require.Equal(t, "b", q.q[1])
}

func TestQueuePrunePastNext(t *testing.T) {
	q := newQueue[uint64, string]()
	q.pushBack("a")
	q.pushBack("b")

	q.prune(10)
	require.Equal(t, uint64(10), q.first)
	require.Equal(t, uint64(10), q.next)
	require.Equal(t, uint64(0), q.Len())
	require.Empty(t, q.q)
}
