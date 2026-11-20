package giga

import (
	"bytes"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

var _ flatkv.RowReader = historicalRows{}

// evmKeyPrefix prefixes the physical key of every FlatKV row of the EVM module.
var evmKeyPrefix = []byte(keys.EVMStoreKey + "/")

// historicalRows reads the FlatKV rows of a past height from the undo log, which records the previous
// value of every mutation FlatKV commits.
type historicalRows struct {
	height int64
	view   *undo.View
}

// GetRaw serves only the EVM module's rows. It panics on any other key, and on a read the undo log cannot
// serve: a view has no recoverable errors.
func (r historicalRows) GetRaw(key []byte) ([]byte, bool) {
	if !bytes.HasPrefix(key, evmKeyPrefix) {
		panic(fmt.Sprintf("giga: no history is served for key %x outside the EVM module", key))
	}
	value, found, err := r.view.GetRaw(key)
	if err != nil {
		panic(fmt.Sprintf("giga: historical read of %x at height %d: %v", key, r.height, err))
	}
	return value, found
}
