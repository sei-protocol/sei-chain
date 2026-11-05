package staking

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

func TestUnbondingEntriesRejectAnImpossibleLength(t *testing.T) {
	s := newStore(newMemState())
	s.SetWord(unbondingBase(alice, val1), solstorage.Slot(1<<32))
	_, err := s.unbondingEntries(alice, val1)
	require.ErrorIs(t, err, errInvalidStoredEntries)
}

func TestQueueClampsToItsLastEntry(t *testing.T) {
	q := newStore(newMemState()).unbondingQueue()
	require.Equal(t, uint64(5), q.clamp(5), "an empty queue takes any time")
	_, err := q.push(10)
	require.NoError(t, err)
	require.Equal(t, uint64(10), q.clamp(5))
	require.Equal(t, uint64(11), q.clamp(11))
	q.pop()
	require.Equal(t, q.head(), q.tail())
	require.Equal(t, uint64(5), q.clamp(5), "a drained queue takes any time")
	_, ok := q.due(100)
	require.False(t, ok)
	require.Equal(t, common.Hash{}, q.s.Word(q.entry(0)), "pop zeroes the entry")
}
