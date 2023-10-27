package ss

import (
	"github.com/sei-protocol/sei-db/ss/pebbledb"
	"github.com/sei-protocol/sei-db/ss/types"
)

func init() {
	initializer := func(dir string) (types.StateStore, error) {
		return pebbledb.New(dir)
	}
	RegisterBackend(PebbleDBBackend, initializer)
}
