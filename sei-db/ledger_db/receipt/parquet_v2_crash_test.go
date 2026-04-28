package receipt

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt/parquet_v2"
	"github.com/stretchr/testify/require"
)

func simulateCrashV2(store ReceiptStore, pqStore *parquet_v2.Store) {
	CloseTxHashIndex(store)
	pqStore.SimulateCrash()
}

func TestParquetV2CrashRecoveryAtEachHookPoint(t *testing.T) {
	type hookSetup struct {
		name          string
		needsRotation bool
		install       func(h *parquet.FaultHooks, trigger func() error)
	}

	hookPoints := []hookSetup{
		{
			name: "AfterWALWrite",
			install: func(h *parquet.FaultHooks, trigger func() error) {
				h.AfterWALWrite = func(uint64) error { return trigger() }
			},
		},
		{
			name: "BeforeFlush",
			install: func(h *parquet.FaultHooks, trigger func() error) {
				h.BeforeFlush = func(uint64) error { return trigger() }
			},
		},
		{
			name: "AfterFlush",
			install: func(h *parquet.FaultHooks, trigger func() error) {
				h.AfterFlush = func(uint64) error { return trigger() }
			},
		},
		{
			name:          "AfterCloseWriters",
			needsRotation: true,
			install: func(h *parquet.FaultHooks, trigger func() error) {
				h.AfterCloseWriters = func(uint64) error { return trigger() }
			},
		},
		{
			name:          "AfterWALClear",
			needsRotation: true,
			install: func(h *parquet.FaultHooks, trigger func() error) {
				h.AfterWALClear = func(uint64) error { return trigger() }
			},
		},
	}

	for _, hp := range hookPoints {
		t.Run(hp.name, func(t *testing.T) {
			ctx, storeKey := newTestContext()
			cfg := dbconfig.DefaultReceiptStoreConfig()
			cfg.Backend = "parquet_v2"
			cfg.DBDirectory = t.TempDir()

			store, err := NewReceiptStore(cfg, storeKey)
			require.NoError(t, err)

			pqStore := extractParquetV2Store(t, store)
			pqStore.SetMaxBlocksPerFile(4)

			preBlocks := uint64(2)
			if hp.needsRotation {
				preBlocks = 3
			}

			addr := common.HexToAddress("0x1")
			for block := uint64(1); block <= preBlocks; block++ {
				txHash := blockTxHash(block)
				receipt := makeTestReceipt(txHash, block, 0, addr, nil)
				require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(block)), []ReceiptRecord{
					{TxHash: txHash, Receipt: receipt},
				}))
			}

			fired := false
			hooks := &parquet.FaultHooks{}
			hp.install(hooks, func() error {
				if !fired {
					fired = true
					return errSimulatedCrash
				}
				return nil
			})
			pqStore.SetFaultHooks(hooks)

			crashBlock := preBlocks + 1
			crashTxHash := blockTxHash(crashBlock)
			crashReceipt := makeTestReceipt(crashTxHash, crashBlock, 0, addr, nil)
			err = store.SetReceipts(ctx.WithBlockHeight(int64(crashBlock)), []ReceiptRecord{
				{TxHash: crashTxHash, Receipt: crashReceipt},
			})
			require.ErrorIs(t, err, errSimulatedCrash)

			simulateCrashV2(store, pqStore)

			store, err = NewReceiptStore(cfg, storeKey)
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			extractParquetV2Store(t, store).SetMaxBlocksPerFile(4)

			for block := uint64(1); block <= crashBlock; block++ {
				txHash := blockTxHash(block)
				got, err := store.GetReceiptFromStore(ctx, txHash)
				require.NoError(t, err, "block %d not recovered", block)
				require.Equal(t, txHash.Hex(), got.TxHashHex)
			}

			postBlock := crashBlock + 1
			postTxHash := blockTxHash(postBlock)
			postReceipt := makeTestReceipt(postTxHash, postBlock, 0, addr, nil)
			require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(postBlock)), []ReceiptRecord{
				{TxHash: postTxHash, Receipt: postReceipt},
			}))
			got, err := store.GetReceiptFromStore(ctx, postTxHash)
			require.NoError(t, err)
			require.Equal(t, postTxHash.Hex(), got.TxHashHex)
		})
	}
}
