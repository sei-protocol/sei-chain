package parquet_v2

import (
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

// Store is the V2 parquet receipt store facade. In the finished implementation
// it will hold only channels into the coordinator goroutine.
type Store struct {
	requests  chan coordRequest
	done      chan struct{}
	closeOnce sync.Once
}

// NewStore creates a non-functional Step 1 V2 store scaffold.
func NewStore(cfg parquet.StoreConfig) (*Store, error) {
	_ = cfg
	return &Store{}, nil
}
