package receipt

import (
	"math/big"
	"testing"

	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/stretchr/testify/require"
)

func TestLittGetBlockStatsWriteRead(t *testing.T) {
	s, cleanup := setupLittCtxStore(t)
	defer cleanup()

	txHash1, r1 := littCtxTestReceipt(1, 0, [20]byte{}, [32]byte{}, 0)
	r1.GasUsed = 10
	r1.EffectiveGasPrice = 200
	txHash2, r2 := littCtxTestReceipt(1, 1, [20]byte{}, [32]byte{}, 0)
	r2.GasUsed = 20

	ctx := newTestCtxAtHeight(1)
	require.NoError(t, s.SetReceipts(ctx, []ReceiptRecord{
		{TxHash: txHash1, Receipt: r1, Reward: big.NewInt(100)},
		{TxHash: txHash2, Receipt: r2}, // no Reward: excluded from the percentile walk
	}))
	requireReceiptVersion(t, s, 1)

	stats, err := s.GetBlockStats(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(30), stats.TotalGasUsed)
	require.Equal(t, uint32(2), stats.TxCount)
	reward, ok := stats.RewardAt(50)
	require.True(t, ok)
	require.Equal(t, uint64(100), reward)
}

func TestLittGetBlockStatsMissingBlockIsUnsupported(t *testing.T) {
	s, cleanup := setupLittCtxStore(t)
	defer cleanup()

	_, err := s.GetBlockStats(newTestCtxAtHeight(1), 999)
	require.ErrorIs(t, err, ErrBlockStatsNotSupported)
}

func TestLittGetBlockStatsBelowRetentionFloorIsNotFound(t *testing.T) {
	s, cleanup := setupLittCtxStore(t)
	defer cleanup()

	// pruneBlocksBelow caps its cutoff at the store's head (never prunes the block just written),
	// so block 1 needs a later block ahead of it before a prune can retire it.
	txHash1, r1 := littCtxTestReceipt(1, 0, [20]byte{}, [32]byte{}, 0)
	require.NoError(t, s.SetReceipts(newTestCtxAtHeight(1), []ReceiptRecord{{TxHash: txHash1, Receipt: r1}}))
	txHash2, r2 := littCtxTestReceipt(2, 0, [20]byte{}, [32]byte{}, 0)
	require.NoError(t, s.SetReceipts(newTestCtxAtHeight(2), []ReceiptRecord{{TxHash: txHash2, Receipt: r2}}))
	requireReceiptVersion(t, s, 2)

	require.NoError(t, s.pruneBlocksBelow(2))

	_, err := s.GetBlockStats(newTestCtxAtHeight(1), 1)
	require.ErrorIs(t, err, ErrNotFound)

	// The physical key is gone too, not merely masked by the floor check.
	_, getErr := s.index.Get(blockStatsKey(1))
	require.Error(t, getErr)

	// Block 2 is at the new floor and still has its stats.
	stats, err := s.GetBlockStats(newTestCtxAtHeight(2), 2)
	require.NoError(t, err)
	require.Equal(t, uint32(1), stats.TxCount)
}

// TestLittGetBlockStatsCorruptDataFallsBack verifies an undecodable stats entry answers
// ErrBlockStatsNotSupported instead of a raw decode error.
func TestLittGetBlockStatsCorruptDataFallsBack(t *testing.T) {
	s, cleanup := setupLittCtxStore(t)
	defer cleanup()

	require.NoError(t, s.index.Set(blockStatsKey(1), []byte{0x01, 0x02}, dbtypes.WriteOptions{}))

	_, err := s.GetBlockStats(newTestCtxAtHeight(1), 1)
	require.ErrorIs(t, err, ErrBlockStatsNotSupported)
}

// TestLittGetBlockStatsForEmptyBlockIsZeroNotUnsupported is the case an executed block with no
// transactions must still answer: a real block that happens to be empty, not a missing one.
func TestLittGetBlockStatsForEmptyBlockIsZeroNotUnsupported(t *testing.T) {
	s, cleanup := setupLittCtxStore(t)
	defer cleanup()

	require.NoError(t, s.SetReceipts(newTestCtxAtHeight(1), nil))
	requireReceiptVersion(t, s, 1)

	stats, err := s.GetBlockStats(newTestCtxAtHeight(1), 1)
	require.NoError(t, err)
	require.Zero(t, stats.TotalGasUsed)
	require.Zero(t, stats.TxCount)
	require.Empty(t, stats.RewardPercentiles)
}

// TestLittWriteEmptyBlockStatsNeverRewritesAnAlreadyCommittedBlock guards against a later,
// out-of-order empty write clobbering a block's already-recorded real stats.
func TestLittWriteEmptyBlockStatsNeverRewritesAnAlreadyCommittedBlock(t *testing.T) {
	s, cleanup := setupLittCtxStore(t)
	defer cleanup()

	txHash, r := littCtxTestReceipt(1, 0, [20]byte{}, [32]byte{}, 0)
	r.GasUsed = 10
	require.NoError(t, s.SetReceipts(newTestCtxAtHeight(1), []ReceiptRecord{{TxHash: txHash, Receipt: r}}))
	requireReceiptVersion(t, s, 1)

	require.NoError(t, s.writeEmptyBlockStats(1))

	stats, err := s.GetBlockStats(newTestCtxAtHeight(1), 1)
	require.NoError(t, err)
	require.Equal(t, uint64(10), stats.TotalGasUsed, "an empty-block write at or below the head must not overwrite real stats")
}
