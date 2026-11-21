package giga

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

var _ flatkv.RowReader = historicalRows{}

// historicalRows reads the FlatKV rows of a past height from the undo log, which records the previous
// value of every mutation FlatKV commits.
type historicalRows struct {
	height int64
	view   *undo.View
}

// GetRaw panics on a read the undo log cannot serve: a view has no recoverable errors.
func (r historicalRows) GetRaw(key []byte) ([]byte, bool) {
	value, found, err := r.view.GetRaw(key)
	if err != nil {
		panic(fmt.Sprintf("giga: historical read of %x at height %d: %v", key, r.height, err))
	}
	return value, found
}
