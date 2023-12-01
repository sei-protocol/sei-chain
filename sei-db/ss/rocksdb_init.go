//go:build rocksdbBackend
// +build rocksdbBackend

package ss

import (
	"github.com/sei-protocol/sei-db/ss/rocksdb"
	"github.com/sei-protocol/sei-db/ss/types"
)

func init() {
	initializer := func(dir string) (types.StateStore, error) {
		return rocksdb.New(dir)
	}
	RegisterBackend(RocksDBBackend, initializer)
}
