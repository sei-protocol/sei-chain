package ss

import (
	"fmt"
	"github.com/sei-protocol/sei-db/ss/pebbledb"
	"github.com/sei-protocol/sei-db/ss/types"
)

func init() {
	fmt.Printf("Init Registering backend: %s\n", PebbleDBBackend)
	initializer := func(dir string) (types.StateStore, error) {
		return pebbledb.New(dir)
	}
	RegisterBackend(PebbleDBBackend, initializer)
}
