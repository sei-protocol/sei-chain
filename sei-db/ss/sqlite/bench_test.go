//go:build sqliteBackend
// +build sqliteBackend

package sqlite

import (
	"testing"

	sstest "github.com/sei-protocol/sei-db/ss/test"
	"github.com/sei-protocol/sei-db/ss/types"
)

func BenchmarkDBBackend(b *testing.B) {
	s := &sstest.StorageBenchSuite{
		NewDB: func(dir string) (types.StateStore, error) {
			return New(dir)
		},
		BenchBackendName: "Sqlite",
	}

	s.BenchmarkGet(b)
	s.BenchmarkApplyChangeset(b)
	s.BenchmarkIterate(b)
}
