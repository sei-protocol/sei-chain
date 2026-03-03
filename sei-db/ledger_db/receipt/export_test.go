package receipt

import (
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	types2 "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// RecoverReceiptStore exposes recoverReceiptStore for testing.
func RecoverReceiptStore(log dbLogger.Logger, changelogPath string, db types2.StateStore) error {
	return recoverReceiptStore(log, changelogPath, db)
}

// GetLogsForTx exposes getLogsForTx for testing.
func GetLogsForTx(receipt *types.Receipt, logStartIndex uint) []*ethtypes.Log {
	return getLogsForTx(receipt, logStartIndex)
}
