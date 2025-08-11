//go:build sqliteBackend
// +build sqliteBackend

package sqlite

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	sstest "github.com/sei-protocol/sei-chain/sei-db/ss/test"
	"github.com/sei-protocol/sei-chain/sei-db/ss/types"
)

func BenchmarkDBBackend(b *testing.B) {
	s := &sstest.StorageBenchSuite{
		NewDB: func(dir string) (types.StateStore, error) {
			return New(dir, config.DefaultStateStoreConfig())
		},
		BenchBackendName: "Sqlite",
	}

	s.BenchmarkGet(b)
	s.BenchmarkApplyChangeset(b)
	s.BenchmarkIterate(b)
}
