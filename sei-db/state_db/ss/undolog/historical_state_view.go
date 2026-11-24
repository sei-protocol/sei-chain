package undolog

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

var _ gigatypes.RawStateView = (*historicalStateView)(nil)

// historicalStateView reads the live state store's key-values as of a past block from the undo log.
type historicalStateView struct {
	view   *undo.View
	height int64
}

func (v *historicalStateView) GetBlockHeight() int64 {
	return v.height
}

// GetRaw returns the bytes the live state store held under key after the view's block, and whether it held any. It
// panics on a read it cannot serve.
func (v *historicalStateView) GetRaw(key []byte) ([]byte, bool) {
	value, err := v.view.Get(key)
	if err != nil {
		panic(fmt.Sprintf("undolog: historical read of %x at height %d: %v", key, v.height, err))
	}
	return value, value != nil
}

func (v *historicalStateView) BatchGetRaw(keys [][]byte) map[string][]byte {
	values := make(map[string][]byte, len(keys))
	for _, key := range keys {
		if value, found := v.GetRaw(key); found {
			values[string(key)] = value
		}
	}
	return values
}

func (v *historicalStateView) Close() {
	v.view.Close()
}
