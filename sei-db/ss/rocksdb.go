//go:build rocksdbBackend
// +build rocksdbBackend

package ss

import (
	"fmt"

	"github.com/sei-protocol/sei-db/ss/rocksdb"
	"github.com/sei-protocol/sei-db/ss/types"
)

func init() {
	fmt.Printf("Init Registering backend: %s\n", PebbleDBBackend)
	initializer := func(dir string) (types.StateStore, error) {
		return rocksdb.New(dir)
	}
	RegisterBackend(PebbleDBBackend, initializer)
}
