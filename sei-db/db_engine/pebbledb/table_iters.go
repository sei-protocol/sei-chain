package pebbledb

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// TableIters returns the number of open SSTable iterators for db.
func TableIters(db types.KeyValueDB) (int64, error) {
	p, ok := db.(*pebbleDB)
	if !ok {
		return 0, fmt.Errorf("expected pebbleDB, got %T", db)
	}
	return p.db.Metrics().TableIters, nil
}
