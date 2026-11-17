package archive

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// openTestReader opens a reader of cfg with its coverage loaded.
func openTestReader(t *testing.T, cfg Config) *Reader {
	r, err := OpenReader(cfg, ReaderOptions{})
	require.NoError(t, err)
	t.Cleanup(r.Close)
	require.NoError(t, r.refreshCoverage(t.Context()))
	return r
}

// writeOrphan inserts the rows of blocks from through to without the commit row that marks them
// archived, as a write interrupted before its commit leaves them.
func writeOrphan(t *testing.T, w *writer, from, to uint64) {
	b := testBatch(t, from, to)
	for _, table := range b.tables() {
		if table.input[0].Data.Rows() == 0 {
			continue
		}
		require.NoError(t, w.insert(t.Context(), table.name, table.input, b.first, b.last))
	}
}

func TestTransactionByHash(t *testing.T) {
	cfg := testConfig(t)
	require.NoError(t, migrate(t.Context(), cfg))
	w := newWriter(cfg)
	defer w.close()
	// Both transactions of each block are locatable, so both payload encodings are read back: the
	// typed envelope of transaction 0 and the RLP list of the legacy transaction 1.
	b := newBatch()
	for n := uint64(1); n <= 3; n++ {
		block := testBlock(n)
		block.Txs[1].Locatable = true
		require.NoError(t, b.add(&block))
	}
	require.NoError(t, w.write(t.Context(), b))
	r := openTestReader(t, cfg)

	for n := uint64(1); n <= 3; n++ {
		block := testBlock(n)
		for i, want := range block.Txs {
			got, err := r.TransactionByHash(t.Context(), want.Receipt.TxHash)
			require.NoError(t, err, "block %d transaction %d", n, i)
			wantBytes, err := want.Tx.MarshalBinary()
			require.NoError(t, err)
			gotBytes, err := got.Tx.MarshalBinary()
			require.NoError(t, err)
			require.Equal(t, wantBytes, gotBytes)
			require.Equal(t, want.Receipt.TxHash, got.Tx.Hash())
			require.Equal(t, want.Sender, got.Sender)
			require.Equal(t, uint32(i), got.Index) //nolint:gosec // two transactions
			require.Equal(t, n, got.Block.Number)
			require.Equal(t, block.Hash, got.Block.Hash)
			require.Equal(t, block.Time, got.Block.Time)
			require.Zero(t, big.NewInt(7).Cmp(got.Block.BaseFee))
		}
	}
}

func TestTransactionByHashNotFound(t *testing.T) {
	cfg := testConfig(t)
	require.NoError(t, migrate(t.Context(), cfg))
	w := newWriter(cfg)
	defer w.close()
	r := openTestReader(t, cfg)
	_, err := r.TransactionByHash(t.Context(), testBlock(1).Txs[0].Receipt.TxHash)
	require.ErrorIs(t, err, ErrNotFound, "an empty archive holds nothing")

	require.NoError(t, w.write(t.Context(), testBatch(t, 1, 2)))
	require.NoError(t, r.refreshCoverage(t.Context()))
	_, err = r.TransactionByHash(t.Context(), common.Hash{0x99})
	require.ErrorIs(t, err, ErrNotFound)
	_, err = r.TransactionByHash(t.Context(), testBlock(1).Txs[1].Receipt.TxHash)
	require.ErrorIs(t, err, ErrNotFound, "a transaction that is not locatable")
}

func TestTransactionByHashSkipsUncommittedBlocks(t *testing.T) {
	cfg := testConfig(t)
	require.NoError(t, migrate(t.Context(), cfg))
	w := newWriter(cfg)
	defer w.close()
	require.NoError(t, w.write(t.Context(), testBatch(t, 1, 2)))
	writeOrphan(t, w, 3, 3)
	r := openTestReader(t, cfg)
	_, err := r.TransactionByHash(t.Context(), testBlock(3).Txs[0].Receipt.TxHash)
	require.ErrorIs(t, err, ErrNotFound, "rows past the last commit")

	// Block 3 is now inside the committed bounds but still has no commit.
	require.NoError(t, w.write(t.Context(), testBatch(t, 4, 4)))
	require.NoError(t, r.refreshCoverage(t.Context()))
	ranges, known := r.coverage.load()
	require.True(t, known)
	require.Equal(t, committedRanges{{1, 2}, {4, 4}}, ranges)
	_, err = r.TransactionByHash(t.Context(), testBlock(3).Txs[0].Receipt.TxHash)
	require.ErrorIs(t, err, ErrNotFound, "rows in a gap between commits")
	for _, n := range []uint64{1, 2, 4} {
		got, err := r.TransactionByHash(t.Context(), testBlock(n).Txs[0].Receipt.TxHash)
		require.NoError(t, err, "block %d", n)
		require.Equal(t, n, got.Block.Number)
	}
}

func TestCommittedRangesMerge(t *testing.T) {
	var c committedRanges
	c = c.merge([]blockRange{{1, 3}, {5, 6}})
	require.Equal(t, committedRanges{{1, 3}, {5, 6}}, c)
	extended := c.merge([]blockRange{{7, 9}, {12, 12}})
	require.Equal(t, committedRanges{{1, 3}, {5, 9}, {12, 12}}, extended, "an adjacent run extends the last range")
	require.Equal(t, committedRanges{{1, 3}, {5, 6}}, c, "merge leaves its receiver unchanged")

	for block, want := range map[uint64]bool{0: false, 1: true, 3: true, 4: false, 8: true, 10: false, 12: true, 13: false} {
		require.Equal(t, want, extended.contains(block), "block %d", block)
	}
	first, last, ok := extended.bounds()
	require.True(t, ok)
	require.Equal(t, [2]uint64{1, 12}, [2]uint64{first, last})
	_, _, ok = committedRanges(nil).bounds()
	require.False(t, ok)
}

func TestReaderWithoutClickHouse(t *testing.T) {
	r, err := OpenReader(Config{Addr: "127.0.0.1:1", Database: "sei_archive_test"}, ReaderOptions{QueryTimeout: time.Second})
	require.NoError(t, err)
	require.Error(t, r.refreshCoverage(t.Context()))
	_, err = r.TransactionByHash(t.Context(), common.Hash{1})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNotFound)

	start := time.Now()
	r.Close()
	require.Less(t, time.Since(start), 5*time.Second)
}
