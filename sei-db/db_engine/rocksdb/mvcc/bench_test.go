//go:build rocksdbBackend
// +build rocksdbBackend

package mvcc

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/test"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
)

func BenchmarkDBBackend(b *testing.B) {
	s := &sstest.StorageBenchSuite{
		NewDB: func(dir string) (types.StateStore, error) {
			return OpenDB(dir, config.DefaultStateStoreConfig())
		},
		BenchBackendName: "RocksDB",
	}

	s.BenchmarkGet(b)
	s.BenchmarkApplyChangeset(b)
	s.BenchmarkIterate(b)
}
