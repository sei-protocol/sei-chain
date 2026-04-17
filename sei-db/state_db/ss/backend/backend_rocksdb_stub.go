//go:build !rocksdbBackend

package backend

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

func openRocksDB(_ string, _ config.StateStoreConfig) (types.StateStore, error) {
	return nil, fmt.Errorf("rocksdb backend not available: rebuild with -tags=rocksdbBackend")
}
