package ss

import (
	"github.com/sei-protocol/sei-db/ss/sqlite"
	"github.com/sei-protocol/sei-db/ss/types"
)

func init() {
	initializer := func(dir string) (types.StateStore, error) {
		return sqlite.New(dir)
	}
	RegisterBackend(SQLiteBackend, initializer)
}
