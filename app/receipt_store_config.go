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
	receiptConfig.KeepRecent = cast.ToInt(appOpts.Get(server.FlagMinRetainBlocks))
	return receiptConfig, nil
}
