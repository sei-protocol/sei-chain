package receipt_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestConditionalReceiptsPreserveCommittedHistory(t *testing.T) {
	for _, backend := range []string{"littidx", "pebble"} {
		for _, buffer := range []int{0, 16} {
			t.Run(fmt.Sprintf("%s/buffer=%d", backend, buffer), func(t *testing.T) {
				key := storetypes.NewKVStoreKey("evm")
				ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("evm_transient"))
				cfg := dbconfig.DefaultReceiptStoreConfig()
				cfg.Backend, cfg.DBDirectory, cfg.KeepRecent, cfg.AsyncWriteBuffer = backend, t.TempDir(), 0, buffer
				store, err := receipt.NewReceiptStore(cfg, key)
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, store.Close()) })
				addr, topic := common.HexToAddress("0xabc"), common.HexToHash("0xdef")
				original := litReceipt(1, 0, addr, topic)
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
				if backend == "littidx" {
					logs, err := store.FilterLogs(ctx, 1, 3, filters.FilterCriteria{
						Addresses: []common.Address{addr}, Topics: [][]common.Hash{{topic}},
					}, nil)
					require.NoError(t, err)
					require.Len(t, logs, 1)
					require.Equal(t, original.TxHash, logs[0].TxHash)
					require.Equal(t, uint64(1), logs[0].BlockNumber)
					require.Equal(t, []walked{{block: 1, txHash: original.TxHash}, {block: 3, txHash: unknown.TxHash}}, drainReceipts(t, store, 1))
				}
			})
		}
	}
}

func TestConditionalReceiptDoesNotReuseRetainedLittKey(t *testing.T) {
	store, ctx := setupLittIdxSync(t)
	original := litReceipt(1, 0, common.HexToAddress("0xabc"))
	require.NoError(t, store.SetReceipts(ctx, []receipt.ReceiptRecord{original}))
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(2), nil))
	require.NoError(t, store.(receipt.VersionPinner).SetEarliestVersion(2))
	_, err := store.GetReceipt(ctx, original.TxHash)
	require.ErrorIs(t, err, receipt.ErrNotFound)
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(3), []receipt.ReceiptRecord{{
		TxHash: original.TxHash, KeepExisting: true,
		Receipt: &types.Receipt{TxHashHex: original.TxHash.Hex(), BlockNumber: 3, VmError: "nonce too low"},
	}}))
	_, err = store.GetReceipt(ctx, original.TxHash)
	require.ErrorIs(t, err, receipt.ErrNotFound)
	require.Empty(t, drainReceipts(t, store, 2))
}
