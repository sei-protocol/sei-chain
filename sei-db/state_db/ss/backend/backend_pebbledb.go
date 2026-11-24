package backend

import (
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
)

var (
	_ types.StateStore           = (*mvcc.Database)(nil)
	_ types.ContextIteratorStore = (*mvcc.Database)(nil)
)

func openPebbleDB(dbHome string, cfg config.StateStoreConfig) (types.StateStore, error) {
	db, err := mvcc.OpenDB(dbHome, cfg)
	if err != nil {
		return nil, err
	}
	return db, nil
}
