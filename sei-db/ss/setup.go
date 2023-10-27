package ss

import "path/filepath"

func SetupStateStore(homePath string, backendType BackendType) StateStore {
	dbDirectory := filepath.Join(homePath, "data", string(backendType))
	database, err := NewStateStoreDB(dbDirectory, backendType)
	if err != nil {
		panic(err)
	}
	return database
}
