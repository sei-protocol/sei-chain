//go:build rocksdbBackend
// +build rocksdbBackend

package mvcc

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/test"
)

func BenchmarkDBBackend(b *testing.B) {
	s := &sstest.StorageBenchSuite{
		NewDB: func(dir string) (db_engine.StateStore, error) {
			return OpenDB(dir, config.DefaultStateStoreConfig())
		},
		BenchBackendName: "RocksDB",
	}

	s.BenchmarkGet(b)
	s.BenchmarkApplyChangeset(b)
	s.BenchmarkIterate(b)
}
