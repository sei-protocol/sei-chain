package gigareceipt

import "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"

// PruneLittIdx runs a synchronous prune on a littidx store, removing every
// block below cutoff. Test-only hook so prune behavior can be asserted without
// waiting on the background interval.
func PruneLittIdx(store receipt.ReceiptStore, cutoff uint64) error {
	return store.(*littReceiptStore).pruneBlocksBelow(cutoff)
}
