package pebble

import (
	sstypes "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
)

// RecoverReceiptStore exposes recoverReceiptStore for testing.
func RecoverReceiptStore(changelogPath string, db sstypes.StateStore) error {
	return recoverReceiptStore(changelogPath, db)
}
