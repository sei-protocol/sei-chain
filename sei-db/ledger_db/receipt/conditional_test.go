package receipt_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestConditionalReceiptsPreserveCommittedHistory(t *testing.T) {
	for _, buffer := range []int{0, 16} {
		t.Run(fmt.Sprintf("buffer=%d", buffer), func(t *testing.T) {
			key := storetypes.NewKVStoreKey("evm")
			ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("evm_transient"))
			cfg := dbconfig.DefaultReceiptStoreConfig()
			cfg.DBDirectory, cfg.KeepRecent, cfg.AsyncWriteBuffer = t.TempDir(), 0, buffer
			store, err := receipt.NewReceiptStore(cfg, key)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			addr, topic := common.HexToAddress("0xabc"), common.HexToHash("0xdef")
			original := conditionalTestReceipt(1, 0, addr, topic)
			original.Receipt.Status = uint32(ethtypes.ReceiptStatusSuccessful)
			original.Receipt.CumulativeGasUsed = 21_000
			stale := receipt.ReceiptRecord{TxHash: original.TxHash, KeepExisting: true, Receipt: &types.Receipt{
				TxHashHex: original.TxHash.Hex(), BlockNumber: 1, TransactionIndex: 1,
				CumulativeGasUsed: 21_000, VmError: "nonce too low",
			}}
			require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(1), []receipt.ReceiptRecord{original, stale}))
			replayed := stale
			copyReceipt := *stale.Receipt
			copyReceipt.BlockNumber, copyReceipt.TransactionIndex, copyReceipt.CumulativeGasUsed = 2, 0, 0
			replayed.Receipt = &copyReceipt
			// Queue the replay without waiting for the original block to land.
			require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(2), []receipt.ReceiptRecord{replayed}))
			unknown := replayed
			unknown.TxHash = common.HexToHash("0x1234")
			unknownReceipt := copyReceipt
			unknownReceipt.TxHashHex, unknownReceipt.BlockNumber = unknown.TxHash.Hex(), 3
			unknown.Receipt = &unknownReceipt
			require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(3), []receipt.ReceiptRecord{unknown}))
			require.Eventually(t, func() bool { return store.LatestVersion() == 3 }, 5*time.Second, time.Millisecond)
			require.NoError(t, store.Close())
			store, err = receipt.NewReceiptStore(cfg, key)
			require.NoError(t, err)
			// Preservation must survive reopening, including a replay after restart.
			restarted := replayed
			restartedReceipt := copyReceipt
			restartedReceipt.BlockNumber = 4
			restarted.Receipt = &restartedReceipt
			require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(4), []receipt.ReceiptRecord{restarted}))
			require.Eventually(t, func() bool { return store.LatestVersion() == 4 }, 5*time.Second, time.Millisecond)
			got, err := store.GetReceipt(ctx, original.TxHash)
			require.NoError(t, err)
			require.Equal(t, original.Receipt, got)
			got, err = store.GetReceipt(ctx, unknown.TxHash)
			require.NoError(t, err)
			require.Equal(t, unknown.Receipt, got)
		})
	}
}

func TestConditionalReceiptYieldsToLaterExecutionInBlock(t *testing.T) {
	key := storetypes.NewKVStoreKey("evm")
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("evm_transient"))
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory, cfg.KeepRecent, cfg.AsyncWriteBuffer = t.TempDir(), 0, 0
	store, err := receipt.NewReceiptStore(cfg, key)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	addr := common.HexToAddress("0xabc")
	executed := conditionalTestReceipt(1, 2, addr)
	executed.Receipt.Status = uint32(ethtypes.ReceiptStatusSuccessful)
	rejected := receipt.ReceiptRecord{TxHash: executed.TxHash, KeepExisting: true, Receipt: &types.Receipt{
		TxHashHex: executed.TxHash.Hex(), BlockNumber: 1, TransactionIndex: 0, VmError: "nonce too high",
	}}
	other := conditionalTestReceipt(1, 1, addr)
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(1), []receipt.ReceiptRecord{rejected, other, executed}))
	require.Eventually(t, func() bool { return store.LatestVersion() == 1 }, 5*time.Second, time.Millisecond)
	got, err := store.GetReceipt(ctx, executed.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), got.Status)
	require.Equal(t, uint32(2), got.TransactionIndex)
	require.Equal(t, executed.Receipt.GasUsed, got.GasUsed)
}

// conditionalTestReceipt builds a receipt with one log carrying the given address and topics.
func conditionalTestReceipt(
	block uint64,
	txIndex uint32,
	addr common.Address,
	topics ...common.Hash,
) receipt.ReceiptRecord {
	topicHex := make([]string, 0, len(topics))
	for _, topic := range topics {
		topicHex = append(topicHex, topic.Hex())
	}
	var txHash common.Hash
	copy(txHash[:], fmt.Sprintf("tx-%d-%d", block, txIndex))
	return receipt.ReceiptRecord{
		TxHash: txHash,
		Receipt: &types.Receipt{
			TxHashHex:        txHash.Hex(),
			BlockNumber:      block,
			TransactionIndex: txIndex,
			GasUsed:          21000,
			Logs: []*types.Log{
				{Address: addr.Hex(), Topics: topicHex, Data: []byte{0xde, 0xad}, Index: 0},
			},
		},
	}
}
