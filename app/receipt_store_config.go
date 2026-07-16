package app

import (
	"github.com/spf13/cast"

	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
)

const (
	receiptStoreBackendKey              = "receipt-store.rs-backend"
	receiptStoreDBDirectoryKey          = "receipt-store.db-directory"
	receiptStoreAsyncWriteBufferKey     = "receipt-store.async-write-buffer"
	receiptStorePruneIntervalSecondsKey = "receipt-store.prune-interval-seconds"
	receiptStoreReadWriteMetricsKey     = "receipt-store.enable-read-write-metrics"
)

func readReceiptStoreConfig(homePath string, appOpts seidbconfig.AppOptions) (seidbconfig.ReceiptStoreConfig, error) {
	receiptConfig, err := seidbconfig.ReadReceiptConfig(appOpts)
	if err != nil {
		return receiptConfig, err
	}
	if receiptConfig.DBDirectory == "" {
		receiptConfig.DBDirectory = utils.GetReceiptStorePath(homePath, receiptConfig.Backend)
	}
	receiptConfig.KeepRecent = cast.ToInt(appOpts.Get(server.FlagMinRetainBlocks))
	return receiptConfig, nil
}
