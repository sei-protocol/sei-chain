package receipt

import (
	types2 "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// RecoverReceiptStore exposes recoverReceiptStore for testing.
func RecoverReceiptStore(changelogPath string, db types2.StateStore) error {
	return recoverReceiptStore(changelogPath, db)
}

// PruneLittIdx runs a synchronous prune on a littidx store, removing every
// block below cutoff. Test-only hook so prune behavior can be asserted without
// waiting on the background interval.
func PruneLittIdx(store ReceiptStore, cutoff uint64) error {
	return store.(*littReceiptStore).pruneBlocksBelow(cutoff)
}
