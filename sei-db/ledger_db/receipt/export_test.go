package receipt

import (
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	types2 "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// RecoverReceiptStore exposes recoverReceiptStore for testing.
func RecoverReceiptStore(changelogPath string, db types2.StateStore) error {
	return recoverReceiptStore(changelogPath, db)
}

// GetLogsForTx exposes getLogsForTx for testing.
func GetLogsForTx(receipt *types.Receipt, logStartIndex uint) []*ethtypes.Log {
	return getLogsForTx(receipt, logStartIndex)
}

// PruneReceiptBackend runs a synchronous prune on a littdb or pebblev3
// receipt store, removing every block strictly below cutoff. Test-only hook
// so prune behavior can be asserted without waiting on the background
// interval.
func PruneReceiptBackend(store ReceiptStore, cutoff uint64) error {
	if c, ok := store.(*cachedReceiptStore); ok {
		store = c.backend
	}
	switch s := store.(type) {
	case *littReceiptStore:
		return s.pruneBlocksBelow(cutoff)
	case *pebbleReceiptStore:
		return s.pruneBlocksBelow(cutoff)
	default:
		return nil
	}
}

// BloomAddForTest / BloomMayContainForTest / BloomMatchesCriteriaForTest expose
// the block bloom primitives for unit tests.
var (
	BloomAddForTest             = bloomAdd
	BloomMayContainForTest      = bloomMayContain
	BloomMatchesCriteriaForTest = bloomMatchesCriteria
	BlockBloomSizeBytesForTest  = blockBloomSizeBytes
)

// CloseTxHashIndex closes the tx hash index held by a parquet receipt store,
// allowing the same directory to be reopened in crash-recovery tests.
func CloseTxHashIndex(store ReceiptStore) {
	if c, ok := store.(*cachedReceiptStore); ok {
		store = c.backend
	}
	if pq, ok := store.(*parquetReceiptStore); ok && pq.txHashIndex != nil {
		if pq.indexPruner != nil {
			pq.indexPruner.Stop()
			pq.indexPruner = nil
		}
		_ = pq.txHashIndex.Close()
		pq.txHashIndex = nil
	}
}
