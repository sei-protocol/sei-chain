package undolog

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
)

// historicalRowReader returns a reader of the live state store's rows as of height, served from the undo log's
// history through view. A view has no recoverable errors, so a read it cannot serve panics.
func historicalRowReader(view *undo.View, height int64) func(physKey []byte) ([]byte, bool) {
	return func(physKey []byte) ([]byte, bool) {
		value, err := view.Get(physKey)
		if err != nil {
			panic(fmt.Sprintf("undolog: historical read of %x at height %d: %v", physKey, height, err))
		}
		return value, value != nil
	}
}
