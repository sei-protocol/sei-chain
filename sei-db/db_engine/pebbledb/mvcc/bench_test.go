package mvcc

import (
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/test"
)

func BenchmarkDBBackend(b *testing.B) {
	s := &sstest.StorageBenchSuite{
		NewDB: func(dir string) (types.StateStore, error) {
			return OpenDB(dir, config.DefaultStateStoreConfig())
		},
		BenchBackendName: "PebbleDB",
	}

	s.BenchmarkGet(b)
	s.BenchmarkApplyChangeset(b)
	s.BenchmarkIterate(b)
}
