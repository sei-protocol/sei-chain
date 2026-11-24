package chstore

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArchiveBlockTakesOnlyTheNextBlock(t *testing.T) {
	e := &Exporter{cur: newBatch(), next: 5}

	for _, n := range []uint64{4, 6} {
		block := testBlock(n)
		require.ErrorContains(t, e.ArchiveBlock(t.Context(), &block), "expects block 5")
	}
	require.Zero(t, e.cur.blocks)

	unencodable := testBlock(5)
	unencodable.Txs[0].Receipt = nil
	require.Error(t, e.ArchiveBlock(t.Context(), &unencodable))
	require.Equal(t, uint64(5), e.next, "a refused block does not advance the archive")
	require.Zero(t, e.cur.blocks)

	block := testBlock(5)
	require.NoError(t, e.ArchiveBlock(t.Context(), &block))
	require.Equal(t, uint64(6), e.next)
	require.Equal(t, 1, e.cur.blocks)

	e.closed = true
	next := testBlock(6)
	require.ErrorContains(t, e.ArchiveBlock(t.Context(), &next), "closed")
}

func TestArchiveBlockRefusesBlocksAfterAFailedWrite(t *testing.T) {
	e := &Exporter{cur: newBatch(), next: 5}
	failed := errors.New("insert failed")
	e.writeErr.Store(&failed)

	block := testBlock(5)
	require.ErrorIs(t, e.ArchiveBlock(t.Context(), &block), failed)
	require.Zero(t, e.cur.blocks)
}
