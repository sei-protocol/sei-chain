package ss

func SetupStateStore(homePath string, backendType BackendType) StateStore {
	database, err := NewStateStoreDB(homePath, backendType)
	if err != nil {
		panic(err)
	}
	return database
}
