package ss

import (
	"path/filepath"

	"github.com/sei-protocol/sei-db/ss/types"
)

func SetupStateStore(homePath string, backendType BackendType) types.StateStore {
	dbDirectory := filepath.Join(homePath, "data", string(backendType))
	database, err := NewStateStoreDB(dbDirectory, backendType)
	if err != nil {
		panic(err)
	}
	return database
}
