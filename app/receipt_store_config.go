package app

import (
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/spf13/cast"
)

const (
	receiptStoreBackendKey              = "receipt-store.rs-backend"
	receiptStoreDBDirectoryKey          = "receipt-store.db-directory"
	receiptStoreAsyncWriteBufferKey     = "receipt-store.async-write-buffer"
	receiptStoreKeepRecentKey           = "receipt-store.keep-recent"
	receiptStorePruneIntervalSecondsKey = "receipt-store.prune-interval-seconds"
)

func readReceiptStoreConfig(homePath string, appOpts seidbconfig.AppOptions) (seidbconfig.ReceiptStoreConfig, error) {
	receiptConfig, err := seidbconfig.ReadReceiptConfig(appOpts)
	if err != nil {
		return receiptConfig, err
	}
	if receiptConfig.DBDirectory == "" {
		receiptConfig.DBDirectory = filepath.Join(homePath, "data", "receipt.db")
	}
	// If keep-recent was not explicitly set in [receipt-store], fall back to
	// min-retain-blocks for backwards compatibility with v6.3.0 which used
	// that flag to control receipt retention. Without this, nodes upgrading
	// from v6.3.0 silently inherit the 100k default and prune old receipts.
	if appOpts.Get(receiptStoreKeepRecentKey) == nil {
		receiptConfig.KeepRecent = cast.ToInt(appOpts.Get(server.FlagMinRetainBlocks))
	}
	return receiptConfig, nil
}
