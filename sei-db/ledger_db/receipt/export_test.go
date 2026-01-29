package receipt

import (
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// NilReceiptStore returns a nil *cachedReceiptStore for testing nil receiver behavior.
func NilReceiptStore() ReceiptStore {
	return (*cachedReceiptStore)(nil)
}

// MatchTopics exposes matchTopics for testing.
func MatchTopics(topics [][]common.Hash, eventTopics []common.Hash) bool {
	return matchTopics(topics, eventTopics)
}

// RecoverReceiptStore exposes recoverReceiptStore for testing.
func RecoverReceiptStore(log dbLogger.Logger, changelogPath string, db *mvcc.Database) error {
	return recoverReceiptStore(log, changelogPath, db)
}

// GetLogsForTx exposes getLogsForTx for testing.
func GetLogsForTx(receipt *types.Receipt, logStartIndex uint) []*ethtypes.Log {
	return getLogsForTx(receipt, logStartIndex)
}
