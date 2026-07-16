package dbcache

import "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"

// Unwrap returns the innermost KeyValueDB, stripping cached wrappers.
func Unwrap(db types.KeyValueDB) types.KeyValueDB {
	for {
		c, ok := db.(*cachedKeyValueDB)
		if !ok {
			return db
		}
		db = c.db
	}
}
