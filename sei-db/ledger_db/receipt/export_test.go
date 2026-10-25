package receipt

import (
	types2 "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// RecoverReceiptStore exposes recoverReceiptStore for testing.
func RecoverReceiptStore(changelogPath string, db types2.StateStore) error {
	return recoverReceiptStore(changelogPath, db)
}
