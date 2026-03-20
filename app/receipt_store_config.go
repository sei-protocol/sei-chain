package app

import (
	"path/filepath"

	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
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
	return receiptConfig, nil
}
