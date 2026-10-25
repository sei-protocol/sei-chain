package gigareceipt

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/stretchr/testify/require"
)

func TestLocalPrunerKeepsDurableRetentionWindow(t *testing.T) {
	for _, tc := range []struct {
		name        string
		durableHead uint64
		writeHead   uint64
		keepRecent  int64
		wantFloor   int64
	}{
		{"partial flush lag", 5, 6, 3, 3},
		{"stalled flusher", 5, 10, 3, 3},
		{"window deeper than durable history", 2, 10, 3, 0},
		{"nothing flushed", 0, 10, 3, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := dbconfig.DefaultReceiptStoreConfig()
			cfg.DBDirectory = t.TempDir()
			cfg.AsyncWriteBuffer = 0
			cfg.KeepRecent = 0
			_, storeKey := newTestContext()
			store, err := newLittReceiptStore(cfg, storeKey)
			require.NoError(t, err)
			s := store.(*littReceiptStore)
			defer func() { require.NoError(t, s.Close()) }()
			stopLittIdxBackground(s)

			for block := uint64(1); block <= tc.writeHead; block++ {
				writeOneReceipt(t, s, block, common.Address{}, common.Hash{})
				if block == tc.durableHead {
					require.NoError(t, s.flushReceipts())
				}
			}
			latest, err := s.GetLatestBlock()
			require.NoError(t, err)
			require.Equal(t, tc.durableHead, latest)
			require.Equal(t, int64(tc.writeHead), s.LatestVersion())

			s.keepRecent = tc.keepRecent
			s.pruneInterval = 600
			s.startPruning()
			// The first pass runs before the stop is observed, so no timer or polling is needed.
			stopLittIdxBackground(s)
			require.Equal(t, tc.wantFloor, s.EarliestVersion())
			require.Equal(t, tc.wantFloor, s.readMeta(receiptEarliestVersionKey))

			for block := uint64(1); block <= tc.writeHead; block++ {
				txHash, _ := littCtxTestReceipt(block, 0, common.Address{}, common.Hash{}, 1)
				got, err := s.GetReceiptFromStore(newTestCtxAtHeight(block), txHash)
				if int64(block) < tc.wantFloor {
					require.ErrorIs(t, err, receipt.ErrNotFound)
					continue
				}
				require.NoError(t, err, "block %d must remain inside the retention window", block)
				require.Equal(t, block, got.BlockNumber)
			}
		})
	}
}
