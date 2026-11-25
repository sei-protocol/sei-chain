package mvcc

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/sstest"
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
